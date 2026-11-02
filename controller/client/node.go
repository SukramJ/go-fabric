// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	mrand "math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
)

// Node is the image of one remote node — matter.js ClientNode
// (node/ClientNode.ts:55-409 at 0ceb202d) with the subscription and
// connection-state half of NetworkClient. It is safe for concurrent use.
type Node struct {
	cfg     Config
	log     *slog.Logger
	clock   exchange.Clock
	rand    func() float64
	store   Store
	persist *persister
	s       *structure
	// subSync serialises the default subscription's synchronisation
	// ([Node.syncAutoSubscribe]) — matter.js's subscriptionChanges queue
	// (NetworkClient.ts:113-115 at 862ad75f) — so Start and
	// SetAutoSubscribe do not both establish one.
	subSync sync.Mutex

	// mu guards the node's mutable state, every field below it, and the
	// image: the structure, its Endpoints and Clusters all share this lock.
	// What the structure emits is delivered after it is released.
	//
	// loadErr is why the stored node failed to load ([Node.LoadError]).
	mu        sync.RWMutex
	loadErr   error
	settings  settings
	record    Record
	started   bool
	connState ConnectionState
	// pendingStates are the connection-state transitions whose
	// ConnectionStateChanged hook has not run yet, in transition order;
	// draining marks the one recompute that delivers them. In matter.js a
	// transition and its emit are one synchronous step
	// (ClientNodeLifecycle.ts:104-110); here the hook runs with mu
	// released, so the queue keeps the order and a hook that re-enters the
	// node cannot deadlock.
	pendingStates []ConnectionState
	draining      bool
	// props are the physical properties, nil until computed after a change;
	// frozen while the node is stopped (ClientNodePhysicalProperties.ts:100-133).
	props  *Properties
	frozen bool
	// closing: Close began; events are dropped (ClientEventEmitter.ts:47-53).
	closing bool
	// erased: Erase forgot the node, its store included. The record and
	// the settings are saved no longer: a peer's descriptor save runs its
	// observers on the peer's worker goroutine from a snapshot taken
	// before the registry unbound the node, and its record write would
	// land after the erase and bring the node back into the store (the
	// TestRegistryDecommission flake, brief CG §3).
	erased bool
	// newlyCommissioned is Internal.isNewlyCommissioned (NetworkClient.ts:385-391).
	newlyCommissioned bool
	// sub is the default subscription, activeSubscription (:380-382).
	sub *Subscription
	// subOwner names the default subscription being established or held —
	// matter.js internal.subscriptionOwner (NetworkClient.ts:164-166,
	// :207, :245): its callbacks and event emits act only while it is
	// current, so a replaced or ended one never acts for its successor.
	subOwner *subscriptionOwner
	// likelyOffline is the latch of :393-398.
	likelyOffline bool
	seeded        bool
	// labelReconciled is NetworkClient's fabricLabelReconciled latch
	// (NetworkClient.ts:268-273): set by the first SubscriptionAlive after
	// Start, cleared by Stop.
	labelReconciled bool
	stopObserve     func()
	warned          map[[2]uint64]bool
	// requestObserver is a test seam: it sees each subscription request the
	// node hands to the interaction.
	requestObserver func(SubscribePaths)
	// icd is the ICD client a [Registry] gives its nodes, nil otherwise;
	// storedICD its stored registration until the registry built it.
	icd       *ICDClient
	storedICD map[uint32][]byte
}

// New builds the node cfg describes and loads its image from the store.
// Mirrors ClientNode.initialize (ClientNode.ts:105-133): synchronous,
// nothing installed but the image. Config.AutoSubscribe true and a
// Config.DefaultSubscription replace the stored settings.
func New(cfg Config) (*Node, error) {
	if cfg.Interaction == nil {
		return nil, fmt.Errorf("%w: no Interaction", ErrConfig)
	}
	return newNode(cfg)
}

// newNode is New without the Interaction check: a [Registry] builds an
// uncommissioned node unbound and binds it when it is commissioned.
func newNode(cfg Config) (*Node, error) {
	n := &Node{cfg: cfg, log: cfg.Logger, clock: cfg.Clock, rand: cfg.Rand, store: cfg.Store, connState: Disconnected, newlyCommissioned: cfg.NewlyCommissioned, warned: map[[2]uint64]bool{}}
	if n.log == nil {
		n.log = slog.New(slog.DiscardHandler)
	}
	n.log = n.log.With(slog.String("node", cfg.ID))
	if n.clock == nil {
		n.clock = exchange.SystemClock{}
	}
	if n.rand == nil {
		n.rand = mrand.Float64
	}
	if n.store == nil {
		n.store = &MemoryStore{}
	}
	n.persist = &persister{store: n.store, nodeID: cfg.ID, interval: cfg.FlushInterval, clock: n.clock, log: n.log, image: &n.mu}
	stored, err := n.store.Load(cfg.ID)
	if err != nil {
		return nil, fmt.Errorf("client: loading node %q: %w", cfg.ID, err)
	}
	for i, ep := range stored {
		if ep.Number != 0 {
			continue
		}
		for _, c := range ep.Clusters {
			switch c.ID {
			case settingsCluster:
				n.settings, err = decodeSettings(c.Values)
			case recordCluster:
				n.record, err = decodeRecord(c.Values)
			case icdCluster:
				n.storedICD = c.Values
			}
			if err != nil {
				return nil, fmt.Errorf("client: loading node %q: %w", cfg.ID, err)
			}
		}
		// The record is no cluster of the image.
		stored[i].Clusters = slices.DeleteFunc(slices.Clone(ep.Clusters), func(c StoredCluster) bool { return c.ID == recordCluster || c.ID == icdCluster })
	}
	n.s = newStructure(&n.mu, n.log, n.persist)
	if err := n.s.loadCache(stored); err != nil {
		return nil, fmt.Errorf("client: loading node %q: %w", cfg.ID, err)
	}
	changed := false
	if cfg.AutoSubscribe && !n.settings.autoSubscribe {
		n.settings.autoSubscribe, changed = true, true
	}
	if cfg.DefaultSubscription != nil {
		n.settings.defaultSubscription, changed = cfg.DefaultSubscription.clone(), true
	}
	if changed {
		n.saveSettingsLocked()
	}
	n.s.subscribedFabricFiltered = n.settings.defaultSubscription == nil || !n.settings.defaultSubscription.NoFabricFilter
	n.deliver(n.s.takeEmissions())
	return n, nil
}

// apply applies a report to the image under the lock and delivers what it
// produced after — ClientStructure.mutate (D8 of the brief: no job queue,
// the callbacks after the lock). emitEvents is mutate's options.emitEvents
// (ClientStructure.ts:412): the report's events reach Hooks.Event and the
// cursor only when it is set.
func (n *Node) apply(sc scope, r *Report, emitEvents bool) {
	n.mu.Lock()
	before := n.configurationVersionsLocked()
	icdBefore := n.icdModeLocked()
	n.s.mutate(sc, r, emitEvents)
	em := n.s.takeEmissions()
	if !n.frozen {
		n.props = nil
	}
	changed := n.changedConfigurationVersionsLocked(before)
	icdAfter := n.icdModeLocked()
	n.mu.Unlock()
	n.deliver(em)
	// IcdManagementClient.operatingMode$Changed (IcdClient.ts:118-123).
	if n.icd != nil && icdBefore != icdAfter {
		n.icd.onOperatingModeChanged()
	}
	if h := n.cfg.Hooks.ConfigurationVersionChanged; h != nil {
		for _, ep := range changed {
			n.structureHook("configuration_version_changed", func() { h(ep) })
		}
	}
}

// Clusters whose ConfigurationVersion (0x0018) [Hooks.ConfigurationVersionChanged]
// follows: the node's BasicInformation and a bridged endpoint's
// BridgedDeviceBasicInformation.
const (
	basicInformationCluster        uint32 = 0x0028
	bridgedBasicInformationCluster uint32 = 0x0039
	attrConfigurationVersion       uint32 = 0x0018
)

// configurationVersionsLocked is every endpoint's ConfigurationVersion in
// the image. The caller holds the lock.
func (n *Node) configurationVersionsLocked() map[uint16]uint64 {
	out := map[uint16]uint64{}
	for no, ep := range n.s.endpoints {
		for _, id := range []uint32{basicInformationCluster, bridgedBasicInformationCluster} {
			if c := ep.clusters[id]; c != nil {
				if v, ok := c.cache.values[attrConfigurationVersion]; ok {
					out[no] = v.El.Uint
				}
			}
		}
	}
	return out
}

// changedConfigurationVersionsLocked is the endpoints whose
// ConfigurationVersion the image held before and holds changed now —
// matter.js Peers #instrumentBasicInformation and
// #instrumentBridgedConfigurationVersion, configurationVersion$Changed
// emitting the endpoint's lifecycle.configurationVersionChanged
// (packages/node/src/node/client/Peers.ts:757, :803-814 at 862ad75f).
// The caller holds the lock.
func (n *Node) changedConfigurationVersionsLocked(before map[uint16]uint64) []*Endpoint {
	var out []*Endpoint
	for no, v := range n.configurationVersionsLocked() {
		if old, ok := before[no]; ok && old != v {
			if ep := n.s.endpoints[no]; ep != nil && ep.installed {
				out = append(out, ep)
			}
		}
	}
	slices.SortFunc(out, func(a, b *Endpoint) int { return int(a.number) - int(b.number) })
	return out
}

// Properties are the node's physical properties, computed from the image
// after each change and frozen while the node is stopped — matter.js
// ClientNodePhysicalProperties (node/client/ClientNodePhysicalProperties.ts:18-133).
func (n *Node) Properties() Properties {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.propertiesLocked()
}

func (n *Node) propertiesLocked() Properties {
	if n.props == nil {
		p := n.s.computeProperties()
		n.props = &p
	}
	return *n.props
}

// IntervalBounds are the reporting interval bounds of a subscription to the
// node, the caller's floor and ceiling (nil for the node's default)
// adjusted to its physical properties —
// PhysicalDeviceProperties.subscriptionIntervalBoundsFor
// (PhysicalDeviceProperties.ts:64-164), as ClientNodeInteraction.subscribe
// applies it (ClientNodeInteraction.ts:129-133).
func (n *Node) IntervalBounds(floor, ceiling *time.Duration) (minFloor, maxCeiling time.Duration) {
	return intervalBounds(n.Properties(), floor, ceiling, n.rand, n.log)
}

// deliver runs the hooks of what the structure emitted, in order.
func (n *Node) deliver(em []emission) {
	h := n.cfg.Hooks
	for i := range em {
		e := &em[i]
		switch e.kind {
		case emitEndpointInstalled:
			if h.EndpointInstalled != nil {
				n.structureHook("endpoint_installed", func() { h.EndpointInstalled(e.ep) })
			}
		case emitClusterInstalled:
			if h.ClusterInstalled != nil {
				n.structureHook("cluster_installed", func() { h.ClusterInstalled(e.ep, e.c) })
			}
		case emitClusterReplaced:
			if h.ClusterReplaced != nil {
				n.structureHook("cluster_replaced", func() { h.ClusterReplaced(e.ep, e.c) })
			}
		case emitClusterDeleted:
			if h.ClusterDeleted != nil {
				n.structureHook("cluster_deleted", func() { h.ClusterDeleted(e.ep, e.c) })
			}
		case emitChanged:
			if h.Changed != nil {
				h.Changed()
			}
			n.evaluateSeeded()
		case emitEvent:
			n.deliverEvent(&e.event)
		}
	}
}

// structureHook runs a structure hook; a panic in it is logged, never
// raised (ClientStructureEvents.ts:138-144).
func (n *Node) structureHook(kind string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			n.log.Error("matter.controller.client.hook_failed", slog.String("hook", kind), slog.Any("panic", r))
		}
	}()
	f()
}

// Endpoints are the image's installed endpoints in number order, endpoint
// 0 first.
func (n *Node) Endpoints() []*Endpoint {
	n.mu.RLock()
	defer n.mu.RUnlock()
	var out []*Endpoint
	for _, no := range slices.Sorted(maps.Keys(n.s.endpoints)) {
		if ep := n.s.endpoints[no]; ep.installed {
			out = append(out, ep)
		}
	}
	return out
}

// Endpoint is installed endpoint number, nil when the image has none.
func (n *Node) Endpoint(number uint16) *Endpoint {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if ep := n.s.endpoints[number]; ep != nil && ep.installed {
		return ep
	}
	return nil
}

// saveSettingsLocked persists the settings; a store error is logged. The
// caller holds n.mu (or owns n exclusively).
func (n *Node) saveSettingsLocked() {
	if n.erased {
		return
	}
	values, err := n.settings.encode()
	if err == nil {
		err = n.store.Save(n.cfg.ID, 0, settingsCluster, values, nil)
	}
	if err != nil {
		n.log.Warn("matter.controller.client.store_write_failed", slog.String("what", "settings"), slog.Any("err", err))
	}
}

// Start brings the node online — ClientNode start through
// NetworkClient.startup (NetworkClient.ts:60-102): the peer's signals are
// bound (:79-83), the default subscription established when AutoSubscribe
// (:88, a newly commissioned node reading first), and the connection state
// recomputed (:98). A disabled node does not start: [ErrDisabled]
// (ClientNetworkRuntime.ts:41-43). An error of the initial read is
// returned; the node stays started.
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	switch {
	case n.loadErr != nil:
		n.mu.Unlock()
		return n.loadErr
	case n.closing:
		n.mu.Unlock()
		return ErrClosed
	case n.settings.disabled:
		n.mu.Unlock()
		return ErrDisabled
	case n.started:
		n.mu.Unlock()
		return nil
	}
	n.started = true
	n.frozen, n.props = false, nil
	n.mu.Unlock()
	n.persist.restart()
	if n.icd != nil {
		// The peer coming online is proof it is awake (IcdClient.ts:156-158).
		n.icd.noteActive()
	}
	stop := n.observePeer()
	n.mu.Lock()
	n.stopObserve = stop
	n.mu.Unlock()
	err := n.syncAutoSubscribe(ctx)
	n.recompute()
	return err
}

// Stop takes the node offline: the default subscription closes
// (NetworkClient.ts:309-313), the peer's signals are released, the
// properties freeze, the store is flushed (ClientCacheBuffer.ts:94-101)
// and the connection state is recomputed. It waits, within ctx, for the
// subscription to end, so a hook must not call it.
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	n.started = false
	n.labelReconciled = false
	sub, stop := n.sub, n.stopObserve
	n.sub, n.stopObserve, n.subOwner = nil, nil, nil
	if !n.frozen {
		n.propertiesLocked()
		n.frozen = true
	}
	n.mu.Unlock()
	if stop != nil {
		stop()
	}
	var err error
	if sub != nil {
		sub.Close()
		select {
		case <-sub.Done():
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	n.persist.stop()
	n.recompute()
	return err
}

// Close stops the node and releases it; it is idempotent. Events that
// arrive meanwhile are dropped.
func (n *Node) Close(ctx context.Context) error {
	n.mu.Lock()
	n.closing = true
	n.mu.Unlock()
	err := n.Stop(ctx)
	if n.icd != nil {
		n.icd.close()
	}
	return err
}

// online is a started, commissioned node — matter.js's ClientNode
// lifecycle.isOnline as the ICD client reads it.
func (n *Node) online() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.started && n.record.PeerAddress != nil
}

// icdModeLocked is the image's IcdManagement OperatingMode, -1 when it
// has none. The caller holds the lock.
func (n *Node) icdModeLocked() int64 {
	if n.s == nil {
		return -1
	}
	ic := readyCluster(n.s.root, icdmanagementClusterID)
	if ic == nil {
		return -1
	}
	if v, ok := uintOf(ic, icdmanagementAttrOperatingMode); ok {
		return int64(v) //nolint:gosec // an enum8
	}
	return -1
}

// SetAutoSubscribe switches the node's default subscription on or off
// and persists the choice with the node's settings: a started node
// establishes the sustained default subscription at once (reading first
// when newly commissioned) or closes it; a stopped one follows the choice
// at its next start — matter.js NetworkClient's autoSubscribe state, whose
// change runs #syncAutoSubscribe (NetworkClient.ts:46, :113-115, :130-196
// at 862ad75f). An unchanged choice is nothing.
func (n *Node) SetAutoSubscribe(ctx context.Context, on bool) error {
	n.mu.Lock()
	if n.settings.autoSubscribe == on {
		n.mu.Unlock()
		return nil
	}
	n.settings.autoSubscribe = on
	n.saveSettingsLocked()
	started := n.started && !n.closing
	n.mu.Unlock()
	if !started {
		return nil
	}
	err := n.syncAutoSubscribe(ctx)
	n.recompute()
	return err
}

// AutoSubscribe reports whether the node keeps the default subscription
// ([Node.SetAutoSubscribe]).
func (n *Node) AutoSubscribe() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.autoSubscribe
}

// Disable takes the node offline and keeps it so, persisted
// (NetworkClient.ts:344-349) — ClientNode.disable (ClientNode.ts:236-250).
func (n *Node) Disable(ctx context.Context) error {
	if n.Disabled() {
		return nil
	}
	if err := n.Stop(ctx); err != nil {
		return err
	}
	// Every subscription of the node goes, not only the default one:
	// ClientNode.disable cancels the node's interaction, which closes the
	// subscriptions it owns (ClientNode.ts:239-253, ClientInteraction.ts:270-282
	// at 862ad75f; ClientConnectivityTest "closes a non-sustained
	// subscription when the node is disabled").
	if ia, err := n.interaction(); err == nil {
		ia.CloseSubscriptions()
	}
	n.mu.Lock()
	n.settings.disabled = true
	n.saveSettingsLocked()
	n.mu.Unlock()
	n.recompute()
	return nil
}

// Enable lifts Disable and starts the node — ClientNode.enable
// (ClientNode.ts:257-264).
func (n *Node) Enable(ctx context.Context) error {
	n.mu.Lock()
	if !n.settings.disabled {
		n.mu.Unlock()
		return nil
	}
	n.settings.disabled = false
	n.saveSettingsLocked()
	n.mu.Unlock()
	return n.Start(ctx)
}

// LoadError is why a stored node failed to load ([ErrLoadFailed] with the
// cause), nil for a node that loaded — matter.js's construction error of
// a peer kept unavailable (Peers.ts:144-161 at 862ad75f).
func (n *Node) LoadError() error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.loadErr
}

// Disabled reports whether the node is disabled.
func (n *Node) Disabled() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.disabled
}

// Erase stops the node and forgets its image, its store included —
// ClientNode.erase (ClientNode.ts:227-229, :266-271).
func (n *Node) Erase(ctx context.Context) error {
	if err := n.Stop(ctx); err != nil {
		return err
	}
	n.mu.Lock()
	n.settings = settings{}
	n.record = Record{}
	for _, ep := range n.s.endpoints {
		for _, c := range ep.clusters {
			c.cache.discard()
		}
	}
	n.s = newStructure(&n.mu, n.log, n.persist)
	n.props = nil
	n.frozen = false
	n.erased = true
	n.mu.Unlock()
	// The store is the host's (SQLite in the reference controller): it is
	// erased with the node's lock released (ADR 0026 P6).
	if err := n.store.EraseNode(n.cfg.ID); err != nil {
		n.log.Warn("matter.controller.client.store_erase_failed", slog.Any("err", err))
	}
	return nil
}

// ConnectionState is the node's connection state
// (ClientNodeLifecycle.ts:80-82).
func (n *Node) ConnectionState() ConnectionState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.connState
}

// ID is the node's local id.
func (n *Node) ID() string { return n.cfg.ID }

// bind sets the node's peer and interaction — a [Registry] binds a node
// it created unbound once the node is commissioned
// (CommissioningClient.ts:637-641, #bindPeer :685-714).
func (n *Node) bind(p *peer.Peer, ia *interaction.Interaction) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.Peer, n.cfg.Interaction = p, ia
}

// interaction is the node's interaction; [ErrNotCommissioned] while the
// node is unbound.
func (n *Node) interaction() (*interaction.Interaction, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.cfg.Interaction == nil {
		return nil, ErrNotCommissioned
	}
	return n.cfg.Interaction, nil
}

// peer is the node's peer, nil while it has none.
func (n *Node) peer() *peer.Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.cfg.Peer
}
