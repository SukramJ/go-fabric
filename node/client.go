// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
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
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// ClientConfig is the node's client stack: what a server node needs to
// reach other nodes of its fabrics — the peers its Binding entries
// direct. matter.js gives every ServerNode its peers (`node.peers`, the
// PeerSet BindingManager.ts:317 and :445 use); here the stack is opt-in,
// an optional sub-system a non-nil [Config].Client builds (ADR 0025
// principle 2).
type ClientConfig struct {
	// Listen is the client socket's bind address; empty is an ephemeral
	// port on every address ("[::]:0", "0.0.0.0:0" with
	// Network.PreferIPv4). The client stack initiates from its own socket:
	// an initiator's source port is free, and the peer answers the source
	// it saw.
	Listen string
	// Names is the DNS-SD cache the peers' operational records are looked
	// up in; nil opens one on the mDNS groups (discovery.Open), which the
	// node closes on Stop. A Names the host supplies stays the host's.
	Names *DiscoveryNames

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
	// resumption is the store's one resumption record per peer, which the
	// CASE responder keeps too (handshake.StoreResumption).
	resumption handshake.ResumptionStore
	// subs is the registry of the subscriptions the node holds on its
	// peers: a bound peer's interaction subscribes through it.
	subs   *interaction.Subscriptions
	stop   context.CancelFunc
	served chan struct{}
}

// newClientStack opens the client socket, its exchange manager and the
// peer set whose fabrics are the node's CASE identities.
//
// The stack states the node's own session parameters — the ones its CASE
// responder states (responderSessionParams): the MRP intervals of
// [Config].Sessions, [Config].MaxPathsPerInvoke and the TCP roles of
// [Config].Network — so a peer learns one set of parameters for this node
// whichever side opened the session (review II, R2-P5-peer-02). With
// Network.TCP.Outgoing it dials TCP for a connection that requires it, a
// Large Message invoke above all (R2-P5-peer-01); the connections hand
// their messages to the stack's exchanges, as the controller's do
// (controller/tcp.go).
func (n *Node) newClientStack(cfg ClientConfig) (*clientStack, error) {
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
	cs.exchanges, err = exchange.NewManager(l, exchange.Config{
		Clock: n.cfg.Clock, Logger: n.logger, Retransmitted: n.retransmitted,
		LocalParameters: responderSessionParams(n.cfg.Network.TCP, n.cfg.MaxPathsPerInvoke, n.cfg.Sessions),
	})
	if err != nil {
		return fail(fmt.Errorf("node: client exchanges: %w", err))
	}
	// The subscriptions a bound peer's interaction opens: matter.js's
	// client node of a server node's peers subscribes through its
	// interaction (NetworkClient.ts:168-210, the sustained default
	// subscription); the reports reach the environment's subscriptions.
	cs.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: n.cfg.Clock, Logger: n.logger})
	cs.subs.Register(cs.exchanges)
	// The node's endpoint tree is served on the sessions this stack opens,
	// as on the bridge's: a peer sends its next request on the session it
	// last received on, which may be one we opened (review II, Fable P-9).
	cs.subs.Serve(n.clientServerConfig())
	if cs.names == nil {
		if cs.names, err = discovery.Open(n.logger); err != nil {
			return fail(fmt.Errorf("node: client discovery: %w", err))
		}
		cs.ownNames = true
	}
	// One resumption record per peer for both roles: the store's, which
	// the CASE responder reads and writes (review II, R2-P5-port-06,
	// F-db021da0-19; matter.js SessionManager.ts:261, :896-908 at
	// db021da0). A node restarted resumes with a bound peer.
	cs.resumption = handshake.NewStoreResumption(n.st)
	pctx := peer.Context{
		Exchanges: cs.exchanges, Names: cs.names, Fabrics: n.clientFabric,
		Resumption: cs.resumption, Logger: n.logger, Clock: n.cfg.Clock,
	}
	if n.cfg.Network.TCP.Outgoing {
		pctx.DialTCP = dialTCP(cs.exchanges, n.logger)
	}
	cs.peers, err = peer.NewSet(pctx)
	if err != nil {
		return fail(fmt.Errorf("node: client peers: %w", err))
	}
	ctx, stop := context.WithCancel(n.life)
	cs.stop, cs.served = stop, make(chan struct{})
	go func() {
		defer close(cs.served)
		// In arrival order per secure session, as controller.New serves
		// its socket (controller/controller.go, openNetwork).
		mgr := cs.exchanges
		_ = l.ServeOrdered(ctx, message.SessionOrderKey, func(buf []byte, src *net.UDPAddr, _ func()) { mgr.HandleDatagram(buf, src) })
	}()
	return cs, nil
}

// clientServerConfig serves the node's endpoint tree on a client stack's
// sessions with the bridge's gates: the bridge's dispatcher of the moment
// (its ACL, privilege, existence and timed sources; nil before Start, a
// request then answers Failure), the bridge's codec, element qualities and
// MaxPathsPerInvoke, and a request context built from the session as the
// bridge builds it for one of its own — fabric, subject with CATs and our
// node id (the server's own stamps; a client-stack session is CASE, never
// PASE), the timed interaction and the Large Message fact, the largest
// payload of the session's messages and its attestation challenge.
//
// The session is named by its identity (core.WithInvokeSession, the
// *exchange.Session), not by a bridge session id: core.WithInvokeSessionID
// names a session of the bridge's registry, whose id space the client stack
// does not share, so a flow that means one (a DiagnosticLogs transfer)
// finds none. What the bridge's server adds beyond the gates is not on this
// path: events are not reported in a read, a SubscribeRequest answers
// InvalidAction, a chunked write (MoreChunkedMessages) is refused whole
// with FAILURE before any of it is applied until ADR 0032's single session
// layer (client/interaction, F-db021da0-51, decision 242), and a write or
// invoke reaches the bridge's subscriptions through the servers' own change
// notifications only.
func (n *Node) clientServerConfig() interaction.ServerConfig {
	maxPaths := int(n.cfg.MaxPathsPerInvoke)
	if maxPaths == 0 {
		maxPaths = im.DefaultMaxPathsPerInvoke
	}
	codec := n.br.Codec()
	return interaction.ServerConfig{
		Dispatch:          n.br.Dispatcher,
		Qualities:         n.br.ElementQualities(),
		MaxPathsPerInvoke: maxPaths,
		// The bridge's codec: the hand-written servers take their own
		// request types, which the generated codec does not produce
		// (review II, wave B round 4).
		FieldsReader: codec.FieldsReader,
		FieldsWriter: codec.FieldsWriter,
		ValueReader:  codec.ValueReader,
		ValueWriter:  codec.ValueWriter,
		RequestContext: func(ctx context.Context, s *exchange.Session) context.Context {
			// The session's identity, which no bridge session shares: the
			// CSR of a fail-safe binds to it (review II, Fable R2-1).
			ctx = mattercore.WithInvokeSession(ctx, s)
			ctx = im.WithMaxPayloadSize(ctx, s.MaxPayloadSize())
			if c := s.AttestationChallenge(); len(c) > 0 {
				ctx = mattercore.WithInvokeAttestationChallenge(ctx, c)
			}
			return ctx
		},
	}
}

// dialTCP is the client stack's TCP dialer: each connection hands its
// messages to the stack's exchanges, as the controller's does
// (controller/tcp.go, dialTCP).
func dialTCP(mgr *exchange.Manager, logger *slog.Logger) peer.DialTCP {
	d := &tcp.Dialer{Logger: logger}
	return func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
		conn, err := d.Dial(ctx, addr.String(), func(conn *tcp.Conn, msg []byte, _ func()) { mgr.HandleChannelMessage(conn, msg) })
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

// peerClient is the client stack a node builds for its own peer path when
// [Config].Client is nil (review II, wizard-2 peer Q2): built at the first
// call that needs it, closed with the node. [Node.Peers] does not return
// it and the binding manager does not use it.
type peerClient struct {
	mu     sync.Mutex
	cs     *clientStack
	closed bool
	// template is the stack's configuration: the zero value (an ephemeral
	// socket, a DNS-SD cache of its own) unless a site set it
	// (internal/nodeseam.UsePeerClient).
	template ClientConfig
}

// errNodeStopped is a peer call after the node stopped.
var errNodeStopped = errors.New("node: stopped")

// peerStack is the client stack the node's peer path establishes on: the
// configured one ([Config].Client), else the node's own, built now when it
// does not exist yet.
func (n *Node) peerStack() (*clientStack, error) {
	if n.client != nil {
		return n.client, nil
	}
	n.peerClient.mu.Lock()
	defer n.peerClient.mu.Unlock()
	if n.peerClient.closed {
		return nil, errNodeStopped
	}
	if n.peerClient.cs == nil {
		cs, err := n.newClientStack(n.peerClient.template)
		if err != nil {
			return nil, err
		}
		n.peerClient.cs = cs
	}
	return n.peerClient.cs, nil
}

// existingPeerStack is the client stack the peer path would use, nil when
// none was built: a lookup of live sessions builds nothing.
func (n *Node) existingPeerStack() *clientStack {
	if n.client != nil {
		return n.client
	}
	n.peerClient.mu.Lock()
	defer n.peerClient.mu.Unlock()
	return n.peerClient.cs
}

// clientStacks is every client stack the node runs: the configured one and
// the peer path's own.
func (n *Node) clientStacks() []*clientStack {
	var out []*clientStack
	if n.client != nil {
		out = append(out, n.client)
	}
	n.peerClient.mu.Lock()
	if n.peerClient.cs != nil {
		out = append(out, n.peerClient.cs)
	}
	n.peerClient.mu.Unlock()
	return out
}

// closeClientStacks closes every client stack at Stop; none is built after.
// work is Stop's in-flight unit, paused while a stack drains.
func (n *Node) closeClientStacks(ctx context.Context, work *countedWork) {
	n.peerClient.mu.Lock()
	n.peerClient.closed = true
	own := n.peerClient.cs
	n.peerClient.cs = nil
	n.peerClient.mu.Unlock()
	if n.client == nil && own == nil {
		return
	}
	work.pause()
	if n.client != nil {
		n.client.close(ctx)
	}
	if own != nil {
		own.close(ctx)
	}
	work.resume()
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
		if verifier, err = peerVerifier(f.rootCert, f.rootPublicKey, n.cfg.Clock); err != nil {
			return handshake.Fabric{}, false
		}
	}
	return handshake.Fabric{Identity: f.identity, RootPublicKey: f.rootPublicKey, Verifier: verifier}, true
}

// forgetClientPeers deletes every client-stack peer of a removed fabric:
// its sessions close, its resumption record and its descriptor go
// (peer.Peer.Delete). Mirrors matter.js, where every NodeSession
// registers with its fabric whatever its role (NodeSession.ts:153-156),
// the fabric's deletion force-closes them all (Fabric.ts:478-488) and
// SessionManager deletes the fabric's resumption records
// (SessionManager.ts:308-310, :537-552 at db021da0).
//
// A RemoveFabric invoked on one of the peer's own client-stack sessions
// gets its response first: that peer is deleted once the response is out
// ([im.DeferAfterResponse]), as the bridge closes its invoking session
// after the response (review II, Fable R2-1).
func (n *Node) forgetClientPeers(ctx context.Context, fabricIndex uint8) {
	invoking := invokingClientSession(ctx)
	for _, cs := range n.clientStacks() {
		for _, p := range cs.peers.Peers() {
			if p.Address().FabricIndex != fabricIndex {
				continue
			}
			if invoking != nil && slices.Contains(p.Sessions(), invoking) {
				later := context.WithoutCancel(ctx)
				if im.DeferAfterResponse(ctx, func() { p.Delete(later) }) {
					continue
				}
			}
			p.Delete(ctx)
		}
	}
}

// invokingClientSession is the client-stack session the request of ctx
// arrived on ([mattercore.WithInvokeSession]), nil when it arrived on
// another stack or none.
func invokingClientSession(ctx context.Context) *exchange.Session {
	s, _ := mattercore.InvokeSessionFromContext(ctx).(*exchange.Session)
	return s
}

// closeClientFabricSessions ends the client stack's connection processes
// and sessions to the peers of a fabric whose operational identity
// UpdateNOC replaced, and forgets their resumption records, so no session
// or resumption made under the old NOC outlives it — it runs after the
// identity moved (Node.fabricUpdated); the peers stay and connect again under the new
// one. Mirrors matter.js FailsafeContext.ts:167-170 (replaceFabric →
// SessionManager.deleteResumptionRecordsForFabric) with the fabric's
// sessions, which every role registers with it (NodeSession.ts:153-156);
// the sessions are closed by the command itself, "close all sessions found
// to the old fabric and just leave the one with this exchange open"
// (packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:372-373
// at db021da0; review II, R2-P6-doc-2910).
func (n *Node) closeClientFabricSessions(ctx context.Context, fabricIndex uint8) {
	invoking := invokingClientSession(ctx)
	for _, cs := range n.clientStacks() {
		for _, p := range cs.peers.Peers() {
			addr := p.Address()
			if addr.FabricIndex != fabricIndex {
				continue
			}
			// A handshake in flight ran under the old NOC: its connection
			// process ends first, then its sessions and record go. The wait
			// is not the command's context's: a teardown whose context has
			// ended would otherwise walk the sessions before a handshake
			// finishing in that instant adopts its own, which then outlives
			// the update; the process's end is bounded by its own waits
			// (dev6a R8).
			p.Disconnect(context.WithoutCancel(ctx), nil)
			for _, s := range cs.exchanges.SessionsFor(addr) {
				// The UpdateNOC's own session closes once its response is
				// out, as the bridge's invoking session does (review II,
				// Fable R2-1).
				if s == invoking {
					mgr, later := cs.exchanges, context.WithoutCancel(ctx)
					if im.DeferAfterResponse(ctx, func() { mgr.CloseSession(later, s) }) {
						continue
					}
				}
				cs.exchanges.CloseSession(ctx, s)
			}
			if _, err := cs.resumption.DeleteResumptionRecord(addr); err != nil {
				n.logger.Warn("case.resumption.remove_failed", slog.Int("fabric_index", int(fabricIndex)), slog.Any("err", err))
			}
		}
	}
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
		// A server the node cannot serve fails the topology before any
		// participant is wired (matter_time.go).
		if err := n.attachMatterTime(t); err != nil {
			return nil, err
		}
		// A store that fails a restore read fails the topology: a server
		// is never served without its stored state, and the topology
		// before stays served (review II, R2-P6-store-2403, -2404).
		if err := n.persistBindings(ctx, t); err != nil {
			return nil, err
		}
		if err := n.persistUserLabels(ctx, t); err != nil {
			return nil, err
		}
		n.persistNonVolatile(ctx, t)
		// What the servers restored for a fabric the table no longer
		// holds goes before the topology is served (fabric_state.go).
		n.reconcileFabricState(ctx, t)
		n.attachFailSafe(ctx, t)
		n.attachTLS(t)
		n.attachNodeServices(t)
		n.bindSync.mu.Lock()
		n.bindSync.topo = t
		running := n.bindSync.running
		n.bindSync.mu.Unlock()
		// Once more, now that a removal's fan-out walks t: a fabric
		// removed after the reconcile above read the table had its
		// fan-out walk the topology before t, and only this second read
		// of the table finds it gone (review II, R2-P2-dr-node-03). A
		// removal whose row goes after this read fans out over t.
		n.reconcileFabricState(ctx, t)
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
// is forgotten. A restore that fails fails the topology, so the server is
// never served without its Save hook (a write answering SUCCESS that is
// not stored) and never restored over a list it served (review II,
// R2-P6-store-2404); the servers restored before it are kept with the
// ones the node met already.
func (n *Node) persistBindings(ctx context.Context, t *endpoint.Topology) error {
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
					if n.bindSync.persisted == nil {
						n.bindSync.persisted = map[*mattercore.Binding]bool{}
					}
					maps.Copy(n.bindSync.persisted, current)
					return err
				}
			}
			current[b] = true
		}
	}
	n.bindSync.persisted = current
	return nil
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
