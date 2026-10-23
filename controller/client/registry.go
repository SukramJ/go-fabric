// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// RegistryConfig is what a [Registry] works with.
type RegistryConfig struct {
	// Store holds every node's image, settings and record; nil is a
	// [MemoryStore].
	Store Store
	// Commissioner commissions the nodes and binds their interactions.
	// Required.
	Commissioner *commissioner.Commissioner
	// Peers is the peer set the commissioner works on. Required.
	Peers *peer.Set
	// Exchanges is the controller's exchange manager. Required.
	Exchanges *exchange.Manager
	// Fabrics are our fabrics — matter.js FabricManager for the registry;
	// the first is the default of a commission. At least one is required.
	Fabrics []*commissioner.Fabric
	// Subscriptions is the controller's subscription registry, the one
	// the commissioner binds.
	Subscriptions *interaction.Subscriptions
	// AutoStart starts every commissioned, enabled node in Start —
	// matter.js NetworkServer.autoStartCommissionedPeers (Peers.ts:155).
	AutoStart bool
	// Node is the template of every node: its Hooks, Logger, Clock, Rand
	// and FlushInterval. ID, Peer, Interaction and Store are the
	// registry's; the commissioning choices (AutoSubscribe,
	// DefaultSubscription, NewlyCommissioned, NoInitialRead) are a
	// commission's ([CommissionOptions]).
	Node Config
	// HooksFor, when set, returns the hooks of the node with local id in
	// place of Node.Hooks, so a host tells its nodes' callbacks apart.
	HooksFor func(id string) Hooks
	// Logger receives the registry's diagnostics; nil discards them.
	Logger *slog.Logger
	// Clock paces the expiry of discovered addresses and nodes; nil is the
	// system clock.
	Clock exchange.Clock
}

// Errors of a [Registry].
var (
	// ErrFabricOperationInProgress: a commission or decommission of the
	// node is already running — matter.js FabricOperationInProgressError
	// (node/client/Peers.ts:81, :597-601).
	ErrFabricOperationInProgress = errors.New("client: a commission or decommission of the node is already in progress")
	// ErrNotCommissioned: the node has no peer address
	// (CommissioningClient.ts:343-345); an unbound node's interactions
	// fail with it too.
	ErrNotCommissioned = errors.New("client: node is not commissioned")
	// ErrAlreadyCommissioned: a commission of a node with a peer address
	// (CommissioningClient.ts:173-175).
	ErrAlreadyCommissioned = errors.New("client: node is already commissioned")
	// ErrNotLocated: a commission without an address
	// (CommissioningClient.ts:220-223).
	ErrNotLocated = errors.New("client: node has not been located")
	// ErrForeignFabric: a peer address of a fabric we do not belong to
	// (Peers.ts:446-448).
	ErrForeignFabric = errors.New("client: peer address of a fabric we do not belong to")
)

// idPrefix is the prefix of an allocated local id — matter.js
// CLIENT_ID_PREFIX (storage/client/ClientNodeStores.ts:23).
const idPrefix = "peer"

// Registry is the controller's set of client nodes — matter.js Peers
// (node/client/Peers.ts:88-894 at 0ceb202d), the ServerNode's container
// of ClientNodes: it restores every stored node, starts the commissioned
// ones, allocates local ids, finds or creates a node for a discovered
// device or a peer address, serializes commission and decommission per
// node, culls expired commissionable nodes and addresses, and reacts to a
// peer's BasicInformation events. It owns its nodes: a node is created
// and deleted through it. It is safe for concurrent use.
type Registry struct {
	cfg   RegistryConfig
	log   *slog.Logger
	clock exchange.Clock
	store Store

	// op is matter.js Peers.#mutex (Peers.ts:91): it serializes the
	// registration of a fabric operation, ForAddress's creation, the cull,
	// a leave and Erase (D8).
	op sync.Mutex

	// mu guards bound, closed, nextID and nodes.
	mu     sync.Mutex
	nodes  []*Node
	bound  map[*Node]func()
	ops    map[*Node]*fabricOp
	leaves map[*Node]*leaveLatch
	nextID int
	timer  exchange.Timer
	closed bool

	// ctx ends the registry's background work on Close.
	ctx    context.Context
	cancel context.CancelFunc
	work   sync.WaitGroup
}

// NewRegistry builds the registry and restores every node its store holds
// — Peers.initialize (Peers.ts:123-152): synchronous, nothing started. A
// commissioned node's protocol peer is bound from its record
// (CommissioningClient.ts:139-142, #bindPeer :685-714). A node that fails
// to load is logged and skipped (:141-147). The id counter moves past
// every stored "peerN" (ClientNodeStores.ts:44-54).
func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	switch {
	case cfg.Commissioner == nil:
		return nil, fmt.Errorf("%w: no Commissioner", ErrConfig)
	case cfg.Peers == nil:
		return nil, fmt.Errorf("%w: no Peers", ErrConfig)
	case cfg.Exchanges == nil:
		return nil, fmt.Errorf("%w: no Exchanges", ErrConfig)
	case len(cfg.Fabrics) == 0 || slices.Contains(cfg.Fabrics, nil):
		return nil, fmt.Errorf("%w: no Fabrics", ErrConfig)
	}
	r := &Registry{cfg: cfg, log: cfg.Logger, clock: cfg.Clock, store: cfg.Store, bound: map[*Node]func(){}, ops: map[*Node]*fabricOp{}, leaves: map[*Node]*leaveLatch{}, nextID: 1}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	if r.clock == nil {
		r.clock = exchange.SystemClock{}
	}
	if r.store == nil {
		r.store = &MemoryStore{}
	}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	ids, err := r.store.NodeIDs()
	if err != nil {
		return nil, fmt.Errorf("client: registry: %w", err)
	}
	for _, id := range ids {
		r.reserveID(id)
		n, err := r.restore(id)
		if err != nil {
			r.log.Warn("matter.controller.client.registry.load_failed", slog.String("node", id), slog.String("err", err.Error()))
			continue
		}
		r.add(n)
	}
	return r, nil
}

// reserveID keeps the counter ahead of a stored "peerN" — the leading
// digits, as Number.parseInt reads them (ClientNodeStores.ts:48-54).
func (r *Registry) reserveID(id string) {
	rest, ok := strings.CutPrefix(id, idPrefix)
	if !ok {
		return
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if num, err := strconv.Atoi(rest[:end]); err == nil && r.nextID <= num {
		r.nextID = num + 1
	}
}

// allocateIDLocked is ClientNodeStores.allocateId (:77-81).
func (r *Registry) allocateIDLocked() string {
	id := idPrefix + strconv.Itoa(r.nextID)
	r.nextID++
	return id
}

// nodeConfig is the template with the registry's fields.
func (r *Registry) nodeConfig(id string) Config {
	cfg := r.cfg.Node
	cfg.ID, cfg.Store, cfg.Peer, cfg.Interaction = id, r.store, nil, nil
	cfg.AutoSubscribe, cfg.DefaultSubscription, cfg.NewlyCommissioned, cfg.NoInitialRead = false, nil, false, false
	if cfg.Logger == nil {
		cfg.Logger = r.log
	}
	return cfg
}

// restore builds a stored node, bound to its peer when its record has a
// peer address.
func (r *Registry) restore(id string) (*Node, error) {
	stored, err := r.store.Load(id)
	if err != nil {
		return nil, err
	}
	var rec Record
	for _, ep := range stored {
		for _, c := range ep.Clusters {
			if ep.Number == 0 && c.ID == recordCluster {
				if rec, err = decodeRecord(c.Values); err != nil {
					return nil, err
				}
			}
		}
	}
	var p *peer.Peer
	if rec.PeerAddress != nil {
		if p, err = r.cfg.Peers.AddKnownPeer(rec.knownDescriptor()); err != nil {
			return nil, err
		}
	}
	return r.build(id, p)
}

// build creates a node of id, bound to p when it is not nil, its hooks
// the registry's.
func (r *Registry) build(id string, p *peer.Peer) (*Node, error) {
	cfg := r.nodeConfig(id)
	holder := &nodeHolder{}
	cfg.Hooks = r.hooksFor(id, holder)
	n, err := newNode(cfg)
	if err != nil {
		return nil, err
	}
	holder.set(n)
	if n.Record().DiscoveredAt.IsZero() {
		// CommissioningClient.initialize (CommissioningClient.ts:125-127).
		now := r.clock.Now()
		n.updateRecord(func(rec *Record) { rec.DiscoveredAt = now })
	}
	if p != nil {
		r.bindPeer(n, p)
	}
	return n, nil
}

// nodeHolder hands a node's wrapped hooks the node they belong to.
type nodeHolder struct {
	// mu guards n.
	mu sync.Mutex
	n  *Node
}

func (h *nodeHolder) set(n *Node) {
	h.mu.Lock()
	h.n = n
	h.mu.Unlock()
}

func (h *nodeHolder) get() *Node {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// bindPeer couples the node with its protocol peer — #bindPeer
// (CommissioningClient.ts:685-714): the node's peer and interaction are
// set, and the record follows the peer's descriptor (#update, :721-741)
// on every descriptor save (peer.updated, :711) and whenever the peer
// takes a session.
func (r *Registry) bindPeer(n *Node, p *peer.Peer) {
	n.bind(p, r.cfg.Commissioner.Interaction(p))
	follow := func() {
		d := p.Descriptor()
		n.updateRecord(func(rec *Record) { rec.applyPeer(d) })
	}
	stop := p.Observe(peer.Observer{
		SessionAdded: func(*exchange.Session) { follow() },
		Updated:      follow,
	})
	r.mu.Lock()
	if old := r.bound[n]; old != nil {
		old()
	}
	r.bound[n] = stop
	r.mu.Unlock()
}

// unbindPeer stops following the node's peer (#unbindPeer, :746-763).
func (r *Registry) unbindPeer(n *Node) {
	r.mu.Lock()
	stop := r.bound[n]
	delete(r.bound, n)
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// add makes n one of the registry's nodes and manages the expiry timer
// (Peers.ts:433-437, #handlePeerAdded :536-541).
func (r *Registry) add(n *Node) {
	r.mu.Lock()
	r.nodes = append(r.nodes, n)
	r.mu.Unlock()
	r.manageExpiration()
}

// remove drops n and manages the expiry timer (Peers.ts:107).
func (r *Registry) remove(n *Node) {
	r.mu.Lock()
	r.nodes = slices.DeleteFunc(r.nodes, func(x *Node) bool { return x == n })
	r.mu.Unlock()
	r.manageExpiration()
}

// deleteNode deletes a node — ClientNode.delete (node/ClientNode.ts:213-225):
// it is closed and forgotten, its store erased, and the protocol peer of
// addr (the node's peer address when nil) deleted even when the teardown
// failed. A node whose teardown failed stays in the registry.
func (r *Registry) deleteNode(ctx context.Context, n *Node, addr *PeerAddress) error {
	if addr == nil {
		if a, ok := n.PeerAddress(); ok {
			addr = &a
		}
	}
	r.unbindPeer(n)
	err := n.Close(ctx)
	// The node's interaction closes with it (ClientNode.ts:288-302,
	// cancelWithMutex: interaction.close()): every client subscription the
	// registry still holds for the peer goes — the default one closed with
	// Stop, the host's own with it.
	if r.cfg.Subscriptions != nil && addr != nil {
		r.cfg.Subscriptions.CloseForPeer(*addr)
	}
	if err == nil {
		err = n.Erase(ctx)
	}
	if addr != nil {
		if p := r.cfg.Peers.Get(*addr); p != nil {
			p.Delete(ctx)
		}
	}
	if err != nil {
		return err
	}
	r.remove(n)
	return nil
}

// Start brings the registry online — Peers.#nodeOnline (Peers.ts:154-169):
// with AutoStart every commissioned, enabled node is started in turn, an
// error logged and the next one started (:157-165); then the expiry timer
// is managed (:168).
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if r.cfg.AutoStart {
		for _, n := range r.Nodes() {
			if !n.IsCommissioned() || n.Disabled() {
				continue
			}
			if err := n.Start(ctx); err != nil {
				r.log.Warn("matter.controller.client.registry.start_failed", slog.String("node", n.ID()), slog.String("err", err.Error()))
			}
		}
	}
	r.manageExpiration()
	return nil
}

// Stop takes the registry offline — Peers.#nodeOffline (Peers.ts:171-176):
// the expiry timer is cancelled and every node stopped in turn.
func (r *Registry) Stop(ctx context.Context) error {
	r.cancelExpiration()
	nodes := r.Nodes()
	errs := make([]error, 0, len(nodes))
	for _, n := range nodes {
		errs = append(errs, n.Stop(ctx))
	}
	return errors.Join(errs...)
}

// Close closes the registry and its nodes — Peers.close (Peers.ts:516-527):
// closed, the expiry timer cancelled, the running fabric operations'
// registrations and the cull drained, every node closed. The stored nodes
// stay. It is idempotent.
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	r.cancelExpiration()
	// The drain of Peers.ts:522: no registration, cull or leave runs past
	// this point.
	func() {
		r.op.Lock()
		defer r.op.Unlock()
	}()
	r.cancel()
	r.work.Wait()
	nodes := r.Nodes()
	errs := make([]error, 0, len(nodes))
	for _, n := range nodes {
		r.unbindPeer(n)
		errs = append(errs, n.Close(ctx))
	}
	return errors.Join(errs...)
}

// Erase deletes every node — Peers.erase (Peers.ts:495-514): one that
// cannot be deleted is closed and dropped instead, its stored data left,
// the failures logged.
func (r *Registry) Erase(ctx context.Context) {
	r.op.Lock()
	defer r.op.Unlock()
	for _, n := range r.Nodes() {
		err := r.deleteNode(ctx, n, nil)
		if err == nil {
			continue
		}
		r.log.Warn("matter.controller.client.registry.delete_failed", slog.String("node", n.ID()), slog.String("err", err.Error()))
		if err := n.Close(ctx); err != nil {
			r.log.Warn("matter.controller.client.registry.close_failed", slog.String("node", n.ID()), slog.String("err", err.Error()))
		}
		r.remove(n)
	}
}

// Nodes are the registry's nodes in the order they joined.
func (r *Registry) Nodes() []*Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.nodes)
}

// Commissioned are the nodes with a peer address — Peers.commissioned
// (Peers.ts:397-400).
func (r *Registry) Commissioned() []*Node {
	var out []*Node
	for _, n := range r.Nodes() {
		if n.IsCommissioned() {
			out = append(out, n)
		}
	}
	return out
}

// Get is the node of local id, nil when there is none.
//
// A local id is not an identity: once a node is gone and its data erased,
// a node created later may receive the same id (ClientNodeStores.ts:71-75).
// Hold a node by its peer address ([Registry.GetByAddress]) for anything
// that outlives a commissioning — and even an address names one device
// only while it stays commissioned (Peers.ts:402-411).
func (r *Registry) Get(id string) *Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.nodes {
		if n.ID() == id {
			return n
		}
	}
	return nil
}

// GetByAddress is the commissioned node with peer address addr, nil when
// there is none (Peers.ts:412-427).
func (r *Registry) GetByAddress(addr PeerAddress) *Node {
	for _, n := range r.Nodes() {
		if a, ok := n.PeerAddress(); ok && a == addr {
			return n
		}
	}
	return nil
}

// Size is the number of nodes.
func (r *Registry) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.nodes)
}

// fabricByIndex is the fabric of our index, nil when we have none —
// FabricManager.maybeFor (NetworkClient.ts:443).
func (r *Registry) fabricByIndex(index uint8) *commissioner.Fabric {
	for _, f := range r.cfg.Fabrics {
		if f.FabricIndex == index {
			return f
		}
	}
	return nil
}

// ForAddress is the node of peer address addr, created when there is none
// — Peers.forAddress (Peers.ts:445-486): the address must be on one of
// our fabrics (:446-448); the creation is serialized (:455-462); a new
// node takes id when given (:464-470) and an allocated one otherwise,
// joins the registry and takes the address, bound to its peer but neither
// started nor auto-subscribed (:472-484).
func (r *Registry) ForAddress(addr PeerAddress, id string) (*Node, error) {
	if r.fabricByIndex(addr.FabricIndex) == nil {
		return nil, ErrForeignFabric
	}
	if n := r.GetByAddress(addr); n != nil {
		return n, nil
	}
	r.op.Lock()
	defer r.op.Unlock()
	if n := r.GetByAddress(addr); n != nil {
		return n, nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	if id == "" {
		id = r.allocateIDLocked()
	} else if slices.ContainsFunc(r.nodes, func(n *Node) bool { return n.ID() == id }) {
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: id %q is taken", ErrConfig, id)
	}
	r.mu.Unlock()
	p, err := r.cfg.Peers.For(addr)
	if err != nil {
		return nil, err
	}
	n, err := r.build(id, nil)
	if err != nil {
		return nil, err
	}
	n.updateRecord(func(rec *Record) {
		a := addr
		rec.PeerAddress = &a
	})
	r.bindPeer(n, p)
	r.add(n)
	return n, nil
}

// hooksFor are a node's hooks: the host's, wrapped by the registry's
// reactions (D6 of brief AL): the registry's work for an event runs
// before the host's Event.
func (r *Registry) hooksFor(id string, holder *nodeHolder) Hooks {
	h := r.cfg.Node.Hooks
	if r.cfg.HooksFor != nil {
		h = r.cfg.HooksFor(id)
	}
	host := h.Event
	h.Event = func(endpoint uint16, cluster, event uint32, e EventData, value any) {
		if n := holder.get(); n != nil {
			r.onEvent(n, endpoint, cluster, event, value)
		}
		if host != nil {
			host(endpoint, cluster, event, e, value)
		}
	}
	hostAlive := h.SubscriptionAlive
	h.SubscriptionAlive = func() {
		if n := holder.get(); n != nil && n.latchLabel() {
			r.goWork(func() { r.reconcileLabel(n) })
		}
		if hostAlive != nil {
			hostAlive()
		}
	}
	return h
}
