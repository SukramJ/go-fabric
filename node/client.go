// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// Client is the node's client stack: what a server node needs to reach
// other nodes of its fabrics — the peers its Binding entries direct.
// matter.js gives every ServerNode its peers (`node.peers`, the PeerSet
// BindingManager.ts:333 and :600 use); here the stack is opt-in.
type Client struct {
	// Enabled builds the client stack; the zero value runs the node
	// without one ([Node.Peers] is nil, a node Binding entry is dropped).
	Enabled bool
	// Listen is the client socket's bind address; empty is an ephemeral
	// port on every address ("[::]:0", "0.0.0.0:0" with
	// Network.PreferIPv4). The client stack initiates from its own socket:
	// an initiator's source port is free, and the peer answers the source
	// it saw.
	Listen string
	// Names is the DNS-SD cache the peers' operational records are looked
	// up in; nil opens one on the mDNS groups (discovery.Open), which the
	// node closes on Stop. A Names the host supplies stays the host's.
	Names *discovery.Names

	// open, set through internal/nodeseam by fabrictest, opens the client
	// socket in place of udp.New: a socket of the site's in-memory network.
	open func() (*udp.Listener, error)
}

// clientStack is the running client stack.
type clientStack struct {
	listener  *udp.Listener
	exchanges *exchange.Manager
	names     *discovery.Names
	ownNames  bool
	peers     *peer.Set
	// subs is the registry of the subscriptions the node holds on its
	// peers: a bound peer's interaction subscribes through it.
	subs   *interaction.Subscriptions
	stop   context.CancelFunc
	served chan struct{}
}

// newClientStack opens the client socket, its exchange manager and the
// peer set whose fabrics are the node's CASE identities.
func (n *Node) newClientStack() (*clientStack, error) {
	cfg := n.cfg.Client
	listen := cfg.Listen
	if listen == "" {
		listen = "[::]:0"
		if n.cfg.Network.PreferIPv4 {
			listen = "0.0.0.0:0"
		}
	}
	var l *udp.Listener
	var err error
	if cfg.open != nil {
		l, err = cfg.open()
	} else {
		l, err = udp.New(udp.Config{LocalAddr: listen, PreferIPv4: n.cfg.Network.PreferIPv4, Logger: n.logger})
	}
	if err != nil {
		return nil, fmt.Errorf("node: client socket: %w", err)
	}
	l.OnPanicRecovered(n.panicRecovered)
	cs := &clientStack{listener: l, names: cfg.Names}
	fail := func(err error) (*clientStack, error) {
		cs.close(n.life)
		return nil, err
	}
	cs.exchanges, err = exchange.NewManager(l, exchange.Config{Clock: n.cfg.Clock, Logger: n.logger, Retransmitted: n.retransmitted})
	if err != nil {
		return fail(fmt.Errorf("node: client exchanges: %w", err))
	}
	// The subscriptions a bound peer's interaction opens: matter.js's
	// client node of a server node's peers subscribes through its
	// interaction (NetworkClient.ts:168-210, the sustained default
	// subscription); the reports reach the environment's subscriptions.
	cs.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: n.cfg.Clock, Logger: n.logger})
	cs.subs.Register(cs.exchanges)
	if cs.names == nil {
		if cs.names, err = discovery.Open(n.logger); err != nil {
			return fail(fmt.Errorf("node: client discovery: %w", err))
		}
		cs.ownNames = true
	}
	// Resumption records stay in memory: the store's resumption table is
	// the responder's, one row per peer, and holds neither the session
	// parameters nor our CATs a client record carries.
	cs.peers, err = peer.NewSet(peer.Context{
		Exchanges: cs.exchanges, Names: cs.names, Fabrics: n.clientFabric,
		Resumption: &peer.MemoryResumptionStore{}, Logger: n.logger, Clock: n.cfg.Clock,
	})
	if err != nil {
		return fail(fmt.Errorf("node: client peers: %w", err))
	}
	ctx, stop := context.WithCancel(n.life)
	cs.stop, cs.served = stop, make(chan struct{})
	go func() {
		defer close(cs.served)
		_ = l.Serve(ctx, cs.exchanges.HandleDatagram)
	}()
	return cs, nil
}

// close releases the client stack: the peers, the exchanges, the socket
// and a DNS-SD cache the node opened.
func (cs *clientStack) close(ctx context.Context) {
	if cs.subs != nil {
		_ = cs.subs.Close(ctx)
	}
	if cs.peers != nil {
		cs.peers.Close(ctx)
	}
	if cs.exchanges != nil {
		_ = cs.exchanges.Shutdown(ctx) // bounded by Stop's context; the socket closes regardless
	}
	if cs.stop != nil {
		cs.stop()
	}
	_ = cs.listener.Close()
	if cs.served != nil {
		<-cs.served
	}
	if cs.ownNames && cs.names != nil {
		_ = cs.names.Close()
	}
}

// clientFabric is our side of a peer's fabric: the node's CASE identity
// on it, its root key and a verifier rooted there — matter.js
// SessionManager.fabricFor (peer.Context.Fabrics).
func (n *Node) clientFabric(addr handshake.PeerAddress) (handshake.Fabric, bool) {
	f, ok := n.caseIDs.forFabric(addr.FabricIndex)
	if !ok {
		return handshake.Fabric{}, false
	}
	verifier, isCert := f.verifier.(*mattercert.Verifier)
	if !isCert {
		var err error
		if verifier, err = mattercert.NewVerifier(f.rootPublicKey, n.cfg.Clock); err != nil {
			return handshake.Fabric{}, false
		}
	}
	return handshake.Fabric{Identity: f.identity, RootPublicKey: f.rootPublicKey, Verifier: verifier}, true
}

// Peers returns the client stack's peer set — the nodes of our fabrics
// the node talks to as a client — or nil when [Config].Client is off.
func (n *Node) Peers() *peer.Set {
	if n.client == nil {
		return nil
	}
	return n.client.peers
}

// Bindings returns the binding manager: it resolves the entries of the
// node's Binding servers into the peers they direct (package binding).
// Without the client stack, a node entry is dropped ("client stack
// disabled"); group and self-bindings resolve as ever.
func (n *Node) Bindings() *binding.Manager { return n.bindings }

// newBindings builds the binding manager over the node.
func (n *Node) newBindings() (*binding.Manager, error) {
	cfg := binding.Config{Topology: nodeTopology{n}, Logger: n.logger, Interaction: interaction.Config{Logger: n.logger, Clock: n.cfg.Clock}}
	// A group entry resolves through the node's group sender (brief AN):
	// the target invokes without an endpoint, suppressResponse forced.
	cfg.Groups = binding.GroupSenderFunc(func(ctx context.Context, fabricIndex uint8, groupID uint16) (binding.GroupTarget, error) {
		s, err := n.groups.Sender(ctx, fabricIndex, groupID)
		if err != nil {
			return nil, err
		}
		return groupTarget{s}, nil
	})
	if n.client != nil {
		cfg.Peers = n.client.peers
		cfg.Interaction.Subscriptions = n.client.subs
	}
	m, err := binding.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("node: bindings: %w", err)
	}
	// A GroupKeyMap or key-set change reloads the fabric's group state,
	// which the group manager announces; the group entries recheck.
	n.groups.OnGroupcastChanged(func(_ context.Context, fabricIndex uint8) { m.GroupKeysChanged(fabricIndex) })
	return m, nil
}

// bindingSync attaches the Binding servers of the topology the bridge
// serves to the binding manager once the node runs.
type bindingSync struct {
	// mu guards topo and running. It is released before the topology is
	// synced to the binding manager.
	mu      sync.Mutex
	topo    *endpoint.Topology
	running bool

	// persistMu guards persisted: the Binding servers whose list the store
	// keeps (persistBindings).
	persistMu sync.Mutex
	persisted map[*mattercore.Binding]bool
}

// observeTopology wraps the bridge's Snapshotter: every topology it hands
// the bridge is the one the binding manager reads and whose Binding
// servers it listens to — matter.js's BindingServer registers with the
// manager as its endpoint comes up and leaves it on disposal
// (BindingServer.ts:120-126, :160-163). Each Binding server's list is
// restored from the store before the bridge serves it and before the
// manager registers its entries (persistBindings).
func (n *Node) observeTopology(inner func(context.Context) (*endpoint.Topology, error)) func(context.Context) (*endpoint.Topology, error) {
	return func(ctx context.Context) (*endpoint.Topology, error) {
		t, err := inner(ctx)
		if err != nil || t == nil {
			return t, err
		}
		n.persistBindings(ctx, t)
		n.persistUserLabels(ctx, t)
		n.attachFailSafe(ctx, t)
		n.bindSync.mu.Lock()
		n.bindSync.topo = t
		running := n.bindSync.running
		n.bindSync.mu.Unlock()
		if running {
			n.syncBindings(t)
		}
		return t, nil
	}
}

// startBindings attaches the current topology's Binding servers; from
// here on every reassembly does.
func (n *Node) startBindings() {
	n.bindSync.mu.Lock()
	n.bindSync.running = true
	t := n.bindSync.topo
	n.bindSync.mu.Unlock()
	if t == nil {
		t = n.br.Topology()
	}
	if t != nil {
		n.syncBindings(t)
	}
}

// persistBindings restores each Binding server of the topology the node
// has not met before from the store and saves its later changes, under its
// endpoint's key (bindingKey) — before the bridge serves the topology and
// before startBindings registers its entries, so the binding manager sees
// the stored entries at start (BindingServer.ts:120-126). A server met
// again in a reassembly is not restored again: its list is the newer (and
// the stored one, since every change is saved). One that left the topology
// is forgotten.
func (n *Node) persistBindings(ctx context.Context, t *endpoint.Topology) {
	n.bindSync.persistMu.Lock()
	defer n.bindSync.persistMu.Unlock()
	current := map[*mattercore.Binding]bool{}
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			b, ok := srv.(*mattercore.Binding)
			if !ok || current[b] {
				continue
			}
			if !n.bindSync.persisted[b] {
				key := bindingKey(ep)
				if err := b.Persist(n.persisted.bindingConfig(context.WithoutCancel(ctx), key)); err != nil {
					n.logger.Warn("binding.restore", slog.String("key", key), slog.Any("err", err))
					continue
				}
			}
			current[b] = true
		}
	}
	n.bindSync.persisted = current
}

// bindingServers are the Binding servers of a topology by endpoint.
func bindingServers(t *endpoint.Topology) map[uint16]*mattercore.Binding {
	out := map[uint16]*mattercore.Binding{}
	if t == nil {
		return out
	}
	for _, ep := range t.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if b, ok := srv.(*mattercore.Binding); ok {
				out[ep.ID] = b
			}
		}
	}
	return out
}

// syncBindings attaches the topology's Binding servers and detaches those
// it no longer has.
func (n *Node) syncBindings(t *endpoint.Topology) {
	want := bindingServers(t)
	for _, ep := range n.bindings.Attached() {
		if _, ok := want[ep]; !ok {
			n.bindings.Detach(ep)
		}
	}
	for ep, b := range want {
		n.bindings.Attach(ep, b)
	}
}

// removeFabricBindings drops a removed fabric's Binding entries from every
// Binding server (FabricScopedDataHandler.ts:109-122).
func (n *Node) removeFabricBindings(fabricIndex uint8) {
	n.bindSync.mu.Lock()
	t := n.bindSync.topo
	n.bindSync.mu.Unlock()
	if t == nil {
		t = n.br.Topology()
	}
	for _, b := range bindingServers(t) {
		b.RemoveFabric(fabricIndex)
	}
}

// nodeTopology is the node as the binding manager reads it.
type nodeTopology struct{ n *Node }

func (t nodeTopology) topology() *endpoint.Topology {
	t.n.bindSync.mu.Lock()
	topo := t.n.bindSync.topo
	t.n.bindSync.mu.Unlock()
	if topo == nil {
		topo = t.n.br.Topology()
	}
	return topo
}

func (t nodeTopology) endpoint(id uint16) *endpoint.Endpoint {
	topo := t.topology()
	if topo == nil {
		return nil
	}
	return topo.FindByID(id)
}

// NodeID implements [binding.Topology]: our node id on the fabric, from
// its CASE identity.
func (t nodeTopology) NodeID(fabricIndex uint8) (uint64, bool) {
	f, ok := t.n.caseIDs.forFabric(fabricIndex)
	if !ok {
		return 0, false
	}
	return f.identity.NodeID, true
}

// HasEndpoint implements [binding.Topology].
func (t nodeTopology) HasEndpoint(id uint16) bool { return t.endpoint(id) != nil }

// HasServer implements [binding.Topology].
func (t nodeTopology) HasServer(id uint16, cluster uint32) bool {
	ep := t.endpoint(id)
	if ep == nil {
		return false
	}
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv != nil && srv.MatterClusterID() == cluster {
			return true
		}
	}
	return false
}

// ClientClusters implements [binding.Topology].
func (t nodeTopology) ClientClusters(id uint16) []uint32 {
	ep := t.endpoint(id)
	if ep == nil {
		return nil
	}
	return append([]uint32(nil), ep.ClientClusters...)
}

// HasGroupKey implements [binding.Topology]: the fabric's GroupKeyMap maps
// the group to a key set it holds (groups.Manager.HasUsableKey).
func (t nodeTopology) HasGroupKey(fabricIndex uint8, groupID uint16) bool {
	ok, err := t.n.groups.HasUsableKey(context.Background(), fabricIndex, groupID)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.n.logger.Debug("binding.group_key.lookup_failed", slog.Any("err", err))
	}
	return err == nil && ok
}

// groupTarget adapts a [groups.Sender] to [binding.GroupTarget].
type groupTarget struct{ s *groups.Sender }

func (g groupTarget) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable) error {
	return g.s.Invoke(ctx, path, fields, groups.RequestOptions{})
}
