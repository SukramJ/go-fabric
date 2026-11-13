// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"bytes"
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/tlv"
)

// defaultRequest is the default subscription — every attribute, every
// event urgent, fabric-filtered, replacing our other subscriptions —
// narrowed by the stored options (NetworkClient.ts:121-128 at 0ceb202d).
// The caller holds the lock.
func (n *Node) defaultRequest() SubscribePaths {
	r := SubscribePaths{
		Attributes:           []AttributePath{{}},
		Events:               []EventPath{{IsUrgent: true}},
		ReplaceSubscriptions: true,
	}
	if o := n.settings.defaultSubscription; o != nil {
		if o.Attributes != nil {
			r.Attributes = slices.Clone(o.Attributes)
		}
		if o.Events != nil {
			r.Events = slices.Clone(o.Events)
		}
		r.NoFabricFilter = o.NoFabricFilter
		r.ReplaceSubscriptions = !o.KeepSubscriptions
		r.MinIntervalFloor, r.MaxIntervalCeiling = o.MinIntervalFloor, o.MaxIntervalCeiling
	}
	return r
}

// endDefaultSubscription ends the default subscription and reports it not
// subscribed if it was reported subscribed. Mirrors matter.js
// NetworkClient.#endDefaultSubscription
// (packages/node/src/behavior/system/network/NetworkClient.ts:238-251 at
// 862ad75f).
func (n *Node) endDefaultSubscription() {
	n.mu.Lock()
	s, wasActive := n.sub, n.subReported
	n.sub, n.subOwner, n.subReported = nil, nil, false
	n.mu.Unlock()
	if s != nil {
		s.Close()
	}
	if wasActive {
		n.announceSubscriptionStatus(false, nil)
	}
}

// syncAutoSubscribe is NetworkClient.#syncAutoSubscribe with the node's
// autoSubscribe, serialised with the other reactions (subSync, the
// subscriptionChanges queue, NetworkClient.ts:113-115 at 862ad75f).
func (n *Node) syncAutoSubscribe(ctx context.Context) error {
	n.subSync.Lock()
	defer n.subSync.Unlock()
	return n.syncAutoSubscribeLocked(ctx, nil)
}

// syncAutoSubscribeLocked is NetworkClient.#syncAutoSubscribe
// (NetworkClient.ts:130-236 at 862ad75f): a newly commissioned node reads
// the default subscription's paths once (:142-151, unless NoInitialRead);
// then, when a subscription is desired and none is held, the default
// subscription is established, sustained, with the event minimum past the
// highest event seen and a bootstrap read unless the initial read ran
// (:163-200); when none is desired the held one ends (:153-160). desire
// overrides the node's autoSubscribe (desiredState, :130). The caller
// holds subSync.
func (n *Node) syncAutoSubscribeLocked(ctx context.Context, desire *bool) error {
	n.mu.Lock()
	desired := n.settings.autoSubscribe && !n.settings.disabled
	if desire != nil {
		desired = *desire && !n.settings.disabled
	}
	newly := n.newlyCommissioned
	n.newlyCommissioned = false
	req := n.defaultRequest()
	held := n.subOwner != nil
	n.mu.Unlock()
	didRead := false
	if newly && !n.cfg.NoInitialRead {
		// The newly-commissioned read emits its events and raises the
		// cursor (NetworkClient.ts:303-308, emitEvents: true).
		if _, err := n.read(ctx, ReadRequest{ReadRequest: ReadPaths{Attributes: req.Attributes, Events: req.Events, NoFabricFilter: req.NoFabricFilter}}, true); err != nil {
			return err
		}
		didRead = true
	}
	if desired == held {
		return nil
	}
	if !desired {
		n.endDefaultSubscription()
		return nil
	}
	return n.establishDefaultSubscription(ctx, req, didRead)
}

// SetDefaultSubscription replaces the node's default subscription and
// persists it with the node's settings; nil is the default (every
// attribute, every event urgent, fabric filtered). The held default
// subscription ends, the cache adopts the new request's fabric filter
// ([Node.adoptDefaultFabricFilter]) and, when the node is started and
// keeps a default subscription, the new one is established — matter.js
// NetworkClient.#handleDefaultSubscriptionChange
// (packages/node/src/behavior/system/network/NetworkClient.ts:117-128 at
// 862ad75f), a reaction to the defaultSubscription state that runs one at
// a time with Start and [Node.SetAutoSubscribe] (:113-115, :88-97). An
// unchanged request is nothing.
func (n *Node) SetDefaultSubscription(ctx context.Context, o *SubscribeOptions) error {
	n.subSync.Lock()
	defer n.subSync.Unlock()
	n.mu.Lock()
	if sameSubscribeOptions(n.settings.defaultSubscription, o) {
		n.mu.Unlock()
		return nil
	}
	n.settings.defaultSubscription = o.clone()
	n.saveSettingsLocked()
	started := n.started && !n.closing
	n.mu.Unlock()
	if started {
		off := false
		if err := n.syncAutoSubscribeLocked(ctx, &off); err != nil {
			return err
		}
	}
	n.adoptDefaultFabricFilter()
	if !started {
		return nil
	}
	err := n.syncAutoSubscribeLocked(ctx, nil)
	n.recompute()
	return err
}

// DefaultSubscription is the node's default subscription as set, nil for
// the default ([Node.SetDefaultSubscription]).
func (n *Node) DefaultSubscription() *SubscribeOptions {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.defaultSubscription.clone()
}

// sameSubscribeOptions reports whether two default subscriptions are the
// same request (their stored forms are equal).
func sameSubscribeOptions(a, b *SubscribeOptions) bool {
	if a == nil || b == nil {
		return a == b
	}
	ea, errA := encodeSubscribeOptions(a)
	eb, errB := encodeSubscribeOptions(b)
	return errA == nil && errB == nil && bytes.Equal(ea, eb)
}

// adoptDefaultFabricFilter makes the fabric filter the default
// subscription sends the filter of the node's cache. The node still starts
// and subscribes after a storage failure: when the cache keeps the other
// filter, it takes no data from that subscription until the switch
// succeeds at the next start or the next change of the default
// subscription. Mirrors matter.js NetworkClient.#adoptFabricFilter
// (packages/node/src/behavior/system/network/NetworkClient.ts:273-298 at
// 862ad75f): a record that failed while the filter is already the cache's
// is a warning, a failed switch an error. The caller holds subSync.
func (n *Node) adoptDefaultFabricFilter() {
	n.mu.RLock()
	filtered := !n.defaultRequest().NoFabricFilter
	n.mu.RUnlock()
	err := n.adoptFabricFilter(filtered)
	if err == nil {
		return
	}
	n.mu.RLock()
	kept := n.s.cacheFabricFiltered() == filtered
	n.mu.RUnlock()
	if kept {
		n.log.Warn("matter.controller.client.fabric_filter_record_failed", slog.String("node", n.cfg.ID), slog.Bool("fabric_filtered", filtered), slog.Any("err", err))
		return
	}
	n.log.Error("matter.controller.client.fabric_filter_switch_failed", slog.String("node", n.cfg.ID), slog.Bool("fabric_filtered", filtered), slog.Any("err", err))
}

// adoptFabricFilter makes filtered the fabric filter of the cache; calls
// run one after the other. Data versions do not depend on the fabric
// filter, but cached values of fabric-scoped data do: when the cache was
// filled under the other filter every cached data version becomes unknown,
// in memory and in the store, while the values stay, and the next full
// report refreshes them. A cache without a recorded filter is trusted to
// hold data of the first filter adopted, which is recorded, its versions
// kept. Each change starts a new epoch, so an interaction that began
// before it records no data version. A failed store write is returned and
// the old record stays — written last, so the next attempt forgets the
// versions again. Mirrors matter.js ClientStructure.adoptFabricFilter
// (packages/node/src/node/client/ClientStructure.ts:343-390 at 862ad75f).
//
// The drain it starts with does not end with the context of the call that
// asked for the adoption (Start, SetDefaultSubscription): an adoption has
// no state for a drain cut short — the caller has stored the default
// subscription the filter belongs to and takes no error for a filter left
// behind it.
//
//nolint:contextcheck // the adoption outlasts its caller's context: it has no cut-short state, see above
func (n *Node) adoptFabricFilter(filtered bool) error {
	n.adoptMu.Lock()
	defer n.adoptMu.Unlock()
	// The deferred report writes and a flush in hand land before a
	// version is forgotten (DatasourceCache.ts:324-325).
	n.jobs.wait()
	n.persist.flushMu.Lock()
	defer n.persist.flushMu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.s
	recorded := s.recordedFabricFiltered
	if recorded != nil && *recorded == filtered {
		return nil
	}
	if recorded == nil {
		if s.cacheFabricFiltered() != filtered {
			s.epoch = &epoch{trusted: true}
		}
		return n.recordCacheFabricFilteredLocked(filtered)
	}
	s.epoch = &epoch{trusted: false}
	defer func() { s.epoch = &epoch{trusted: true} }()
	for _, epNo := range slices.Sorted(maps.Keys(s.endpoints)) {
		ep := s.endpoints[epNo]
		for _, id := range slices.Sorted(maps.Keys(ep.clusters)) {
			// Also when unknown in memory only: the store can still hold a
			// version of the old filter (:381).
			if c := ep.clusters[id]; c.cache != nil {
				if err := c.cache.forgetVersion(); err != nil {
					return err
				}
			}
		}
	}
	return n.recordCacheFabricFilteredLocked(filtered)
}

// recordCacheFabricFilteredLocked records the cache's fabric filter in the
// store — ClientNodeStore.recordCacheFabricFiltered
// (packages/node/src/storage/client/ClientNodeStore.ts:88-97 at 862ad75f),
// under the settings' place. The caller holds n.mu.
func (n *Node) recordCacheFabricFilteredLocked(filtered bool) error {
	if n.erased {
		return nil
	}
	e := tlv.NewEncoder()
	e.PutBool(tlv.AnonymousTag(), filtered)
	b, err := e.Bytes()
	if err == nil {
		err = n.store.Save(n.cfg.ID, 0, settingsCluster, map[uint32][]byte{settingCacheFabricFiltered: b}, nil)
	}
	if err != nil {
		return err
	}
	n.s.recordedFabricFiltered = &filtered
	return nil
}

// establishDefaultSubscription establishes the default subscription of
// req, sustained, and adopts it while it is still current — the tail of
// NetworkClient.#syncAutoSubscribe (NetworkClient.ts:130-236).
func (n *Node) establishDefaultSubscription(ctx context.Context, req SubscribePaths, didRead bool) error {
	sreq := req
	sreq.Sustain = true
	n.mu.RLock()
	sreq.EventFilters = []EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}}
	n.mu.RUnlock()
	// Mirrors matter.js NetworkClient.ts:164-173: the default subscription
	// emits events (and raises the cursor) only while it is current.
	owner := &subscriptionOwner{}
	isCurrent := func() bool {
		n.mu.RLock()
		defer n.mu.RUnlock()
		return n.subOwner == owner
	}
	if !didRead {
		sreq.BootstrapRead = func(ctx context.Context) error { return n.bootstrapRead(ctx, req, isCurrent()) }
	}
	var sub *Subscription
	// The callbacks act only while this is the node's default
	// subscription, so a replaced or ended one never acts for its
	// successor (NetworkClient.ts:162-166, :176-200, :216-232 at 862ad75f).
	alive := func() {
		n.mu.RLock()
		current := n.subOwner == owner
		n.mu.RUnlock()
		if !current {
			return
		}
		n.noteICDActive()
		n.queueAlive(owner, true)
	}
	n.icdSustainHooks(&sreq)
	sreq.Updated = func(*Report) error { alive(); return nil }
	sreq.KeepaliveReceived = alive
	sreq.Closed = func() {
		if isCurrent() {
			n.endDefaultSubscription()
		}
	}
	sreq.ActiveChanged = func(active bool) {
		n.mu.Lock()
		if n.subOwner != owner {
			n.mu.Unlock()
			return
		}
		n.subReported = active
		n.mu.Unlock()
		n.announceSubscriptionStatus(active, owner)
	}
	// A Stop (or Close) that ran during the initial read leaves the node
	// stopped: no default subscription is established on it. Mirrors
	// matter.js NetworkClient.ts:113-131 (each sync re-reads the node's
	// runtime) (W4-ctlim-09).
	n.mu.Lock()
	if !n.started || n.closing {
		n.mu.Unlock()
		return nil
	}
	n.subOwner = owner
	n.mu.Unlock()
	s, err := n.subscribe(ctx, SubscribeRequest{SubscribeRequest: sreq}, isCurrent)
	if err != nil {
		n.mu.Lock()
		if n.subOwner == owner {
			n.subOwner = nil
		}
		n.mu.Unlock()
		return err
	}
	// Ended or replaced while it was being established — Stop cleared the
	// owner: the new subscription closes instead of running on a stopped
	// node (NetworkClient.ts:217-221, W4-ctlim-09).
	if !n.adoptDefaultSubscription(s, &sub, owner) {
		s.Close()
	}
	return nil
}

// announceSubscriptionStatus is subscriptionStatusChanged with its
// reactions (NetworkClient.ts:226-235): the status hook, the ICD client,
// and on going live the alive hook of owner's subscription and a flush of
// the cache.
func (n *Node) announceSubscriptionStatus(active bool, owner *subscriptionOwner) {
	if h := n.cfg.Hooks.SubscriptionStatusChanged; h != nil {
		h(active)
	}
	if n.icd != nil {
		n.icd.onSubscriptionActive(active)
	}
	if active {
		// matter.js goes live only once the priming report's updated has
		// drained (ClientInteraction.ts:1173-1174, SustainedSubscription.ts:224,
		// :282 at db021da0), and emits alive after that
		// (NetworkClient.ts:224-234); here the priming report's hooks may
		// still be queued, so the alive queues behind them. It needs no
		// subscription id: the priming report's own alive has none yet.
		n.queueAlive(owner, false)
		if n.cfg.FlushInterval > 0 {
			n.persist.flush()
		}
	}
	n.recompute()
}

// queueAlive runs Hooks.SubscriptionAlive on the node's job queue, behind
// the store writes and hooks of the reports before it: matter.js's
// updated iterates the whole mutate — its drained queue, the structural
// announcements, the delayed events — before it emits subscriptionAlive
// (packages/node/src/behavior/system/network/NetworkClient.ts:179-190,
// packages/node/src/node/client/ClientStructure.ts:482-497 at db021da0),
// so a consumer's batch end follows every change and event of the batch.
// Called inline, it ran before the report's EndpointInstalled and Event
// hooks, and the camera service's sweep missed an endpoint the batch added
// (brief FP2 §2). A keepalive's alive queues behind the reports before it.
// The job asks what matter.js asks after the drain (NetworkClient.ts:184-187):
// whether owner's subscription is still the node's, and, for a report's
// alive (needID), whether it has a subscription id — so a subscription
// ended meanwhile (Stop, a replacement) emits no alive for its successor.
func (n *Node) queueAlive(owner *subscriptionOwner, needID bool) {
	h := n.cfg.Hooks.SubscriptionAlive
	if h == nil {
		return
	}
	n.jobs.enqueue(func() {
		n.mu.RLock()
		s, current := n.sub, n.subOwner == owner
		n.mu.RUnlock()
		if !current || (needID && (s == nil || s.ID() == interaction.NoSubscription)) {
			return
		}
		h()
	})
}

// subscriptionOwner is the identity of one default subscription; not
// zero-sized, so two owners never share an address.
type subscriptionOwner struct{ _ byte }

// adoptDefaultSubscription makes s the default subscription, also in the
// caller's own variable its callbacks compare against. s can go
// active before n.sub names it, and an ICD trigger that ran then read
// SubscriptionActive false: the ICD client gets the edge again now that it
// can see it (a duplicate trigger is absorbed by its own guards).
//
// It adopts s only while owner is current, checked under the lock that
// Stop clears the owner under, and reports whether it did.
func (n *Node) adoptDefaultSubscription(s *Subscription, own **Subscription, owner *subscriptionOwner) bool {
	n.mu.Lock()
	if n.subOwner != owner {
		n.mu.Unlock()
		return false
	}
	*own, n.sub = s, s
	n.mu.Unlock()
	if s.Active() && n.icd != nil {
		n.icd.onSubscriptionActive(true)
	}
	return true
}

// bootstrapRead is the read a sustained default subscription performs
// before its first attempt — SustainedSubscription.ts:187-204 reading the
// subscription's request, its version filters and event minimum, and
// handing the result to the request's updated (here: the image).
func (n *Node) bootstrapRead(ctx context.Context, req SubscribePaths, emitEvents bool) error {
	sc := newScope(req.Attributes, req.NoFabricFilter)
	n.mu.RLock()
	r := ReadPaths{
		Attributes: req.Attributes, Events: req.Events, NoFabricFilter: req.NoFabricFilter,
		EventFilters:       []EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}},
		DataVersionFilters: n.s.withKnownVersions(sc, nil, false),
	}
	in := n.s.epoch
	n.mu.RUnlock()
	ia, err := n.interaction()
	if err != nil {
		return err
	}
	rep, err := ia.ReadWith(ctx, r)
	if err != nil {
		return err
	}
	n.applyIn(sc, rep, emitEvents, in)
	return nil
}

// SubscriptionActive reports whether the default subscription holds an
// established subscription (NetworkClient.ts:203-209).
func (n *Node) SubscriptionActive() bool {
	n.mu.RLock()
	s := n.sub
	n.mu.RUnlock()
	return s != nil && s.Active()
}

// observePeer binds the peer's signals (NetworkClient.ts:84-88 at
// db021da0): an unresponsive establishment sets the likely-offline latch
// (:404-407), a new session clears it (:414-417), a deleted session and a
// change of the peer's operational service recompute the connection state —
// the service's addresses decide whether the peer has any address to try
// (:88, :390-392).
func (n *Node) observePeer() func() {
	p := n.peer()
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	if svc := p.Service(); svc != nil {
		changes := svc.Changes(ctx)
		go func() {
			for range changes {
				n.recompute()
			}
		}()
	}
	stop := p.Observe(peer.Observer{
		SessionAdded: func(*exchange.Session) {
			n.mu.Lock()
			n.likelyOffline = false
			n.mu.Unlock()
			n.recompute()
		},
		SessionDeleted: func(*exchange.Session) { n.recompute() },
		EstablishmentUnresponsive: func() {
			n.mu.Lock()
			n.likelyOffline = true
			n.mu.Unlock()
			n.recompute()
		},
	})
	return func() {
		stop()
		cancel()
	}
}

// hasAnyAddress reports whether the peer has anything to try: a session,
// an operational address or an address of its operational service.
// Without one no establishment runs, so the establishment-unresponsive
// signal that marks the peer likely offline never fires. Mirrors matter.js
// NetworkClient.ts:541-546 (hasAnyAddress) at db021da0.
func hasAnyAddress(p *peer.Peer) bool {
	if len(p.Sessions()) > 0 || p.Descriptor().OperationalAddress != nil {
		return true
	}
	svc := p.Service()
	return svc != nil && len(svc.Addresses()) > 0
}

// recompute sets the connection state from the liveness signals —
// NetworkClient.#recomputeConnectionState (NetworkClient.ts:374-398 at
// db021da0): an active subscription with a session of the peer is
// Connected (:383-385), a disabled or stopped node Disconnected
// (:386-389), the latch or a peer without any address to try
// WaitingForDeviceDiscovery (:390-392), anything else Reconnecting; a
// transition calls ConnectionStateChanged (ClientNodeLifecycle.ts:101-107).
//
// Invariant: recompute never runs on the discovery cache's loop goroutine.
// hasAnyAddress reads the service's addresses through the loop
// (discovery.Service.Addresses, a loop call that is not re-entrant), so a
// recompute from inside a loop callback would deadlock; the callers are
// the peer's observers, the service's Changes channel drained on a
// goroutine of its own, and the node's own lifecycle and subscription
// paths.
func (n *Node) recompute() {
	p := n.peer()
	noAddress := p != nil && !hasAnyAddress(p)
	n.mu.Lock()
	active := n.sub != nil && n.sub.Active()
	hasSession := p == nil || len(p.Sessions()) > 0
	st := connectionStateFor(active, hasSession, n.settings.disabled || !n.started, n.likelyOffline || noAddress)
	if st == Connected || st == Disconnected {
		n.likelyOffline = false
	}
	changed := st != n.connState
	n.connState = st
	if changed && n.cfg.Hooks.ConnectionStateChanged != nil {
		n.pendingStates = append(n.pendingStates, st)
	}
	if n.draining || len(n.pendingStates) == 0 {
		n.mu.Unlock()
		return
	}
	n.draining = true
	n.mu.Unlock()
	// Deliver the queued transitions in order, mu released around each
	// hook; a transition that arrives meanwhile — from another goroutine or
	// from the hook itself — joins the queue and is delivered here.
	for {
		n.mu.Lock()
		if len(n.pendingStates) == 0 {
			n.draining = false
			n.mu.Unlock()
			return
		}
		next := n.pendingStates[0]
		n.pendingStates = n.pendingStates[1:]
		n.mu.Unlock()
		n.cfg.Hooks.ConnectionStateChanged(next)
	}
}

// connectionStateFor is the decision of NetworkClient.ts:383-396 at
// db021da0; waiting is the latch or a peer without any address.
func connectionStateFor(active, hasSession, offline, waiting bool) ConnectionState {
	switch {
	case active && hasSession:
		return Connected
	case offline:
		return Disconnected
	case waiting:
		return WaitingForDeviceDiscovery
	}
	return Reconnecting
}

// deliverEvent hands an event to the Event hook — ClientEventEmitter
// (node/client/ClientEventEmitter.ts:41-91): dropped while the node closes
// (:47-53); decoded through the event's definition; an unknown cluster or
// event noted once (:156-175) and delivered raw; the event cursor raised
// and persisted (:70-76).
func (n *Node) deliverEvent(e *EventData) {
	n.mu.RLock()
	closing := n.closing
	n.mu.RUnlock()
	path := e.Path
	if closing {
		n.log.Debug("matter.controller.client.event_ignored", slog.Uint64("cluster", uint64(path.Cluster)), slog.Uint64("event", uint64(path.Event)))
		return
	}
	var value any
	if e.HasValue {
		value = e.Value
	}
	def := spec.Lookup(path.Cluster)
	var ev *spec.Event
	if def != nil {
		ev = def.Event(path.Event)
	}
	switch {
	case def == nil:
		n.noteUnknown(path.Cluster, 0, false)
	case ev == nil:
		n.noteUnknown(path.Cluster, path.Event, true)
	case e.HasValue && ev.Decode != nil:
		if v, err := ev.Decode(e.Value); err == nil {
			value = v
		} else {
			n.log.Warn("matter.controller.client.event_undecodable", slog.Uint64("cluster", uint64(path.Cluster)), slog.Uint64("event", uint64(path.Event)), slog.Any("err", err))
		}
	}
	n.events.record(RecordedEvent{EventData: *e, Decoded: value})
	if h := n.cfg.Hooks.Event; h != nil {
		h(path.Endpoint, path.Cluster, path.Event, *e, value)
	}
	n.mu.Lock()
	if e.Number > n.settings.maxEventNumber {
		n.settings.maxEventNumber = e.Number
		n.saveSettingsLocked()
	}
	n.mu.Unlock()
}

// noteUnknown logs an unknown cluster, or an unknown event of a cluster,
// once (ClientEventEmitter.ts:138-156).
func (n *Node) noteUnknown(cluster, event uint32, isEvent bool) {
	key := [2]uint64{uint64(cluster), 1 << 32}
	if isEvent {
		key[1] = uint64(event)
	}
	n.mu.Lock()
	seen := n.warned[key]
	n.warned[key] = true
	n.mu.Unlock()
	if seen {
		return
	}
	if isEvent {
		n.log.Info("matter.controller.client.unknown_event", slog.Uint64("cluster", uint64(cluster)), slog.Uint64("event", uint64(event)))
		return
	}
	n.log.Info("matter.controller.client.unknown_cluster_events", slog.Uint64("cluster", uint64(cluster)))
}

// evaluateSeeded marks the node seeded once its structure is known.
// Mirrors matter.js packages/node/src/node/client/Peers.ts:760-797
// (#evaluateSeeded at 862ad75f): BasicInformation is present and every
// endpoint the root's reported PartsList names (the valid endpoint
// numbers) is installed — an empty PartsList is a node of its root only;
// a node whose PartsList was not reported needs an endpoint beyond the
// root. A root-only node whose reported DeviceTypeList names nothing but
// Root Node is logged as offering no function beyond node management
// (:786-796).
func (n *Node) evaluateSeeded() {
	n.mu.Lock()
	if n.seeded || n.s.root == nil || readyCluster(n.s.root, basicinformation.ClusterID) == nil {
		n.mu.Unlock()
		return
	}
	var desc *Cluster
	if c := n.s.root.clusters[descriptor.ClusterID]; c != nil && c.cache != nil {
		desc = c
	}
	parts, partsReported := reportedParts(desc)
	if partsReported {
		for _, no := range parts {
			if ep := n.s.endpoints[no]; ep == nil || !ep.installed {
				n.mu.Unlock()
				return
			}
		}
	} else {
		installed := 0
		for _, ep := range n.s.endpoints {
			if ep.installed {
				installed++
			}
		}
		if installed <= 1 {
			n.mu.Unlock()
			return
		}
	}
	n.seeded = true
	rootOnly := partsReported && len(parts) == 0 && rootDeviceTypesOnly(desc)
	n.mu.Unlock()
	if rootOnly {
		n.log.Warn("matter.controller.client.root_only",
			slog.String("node", n.cfg.ID),
			slog.String("reason", "has no endpoint besides its root and no device type besides Root Node, so it offers no function beyond node management"))
	}
	if h := n.cfg.Hooks.Seeded; h != nil {
		h()
	}
}

// reportedParts is the root Descriptor's PartsList, its valid endpoint
// numbers (0 to 0xFFFE, EndpointNumber.isValid), and whether the peer
// reported it.
func reportedParts(desc *Cluster) ([]uint16, bool) {
	if desc == nil {
		return nil, false
	}
	v, ok := desc.cache.values[descriptor.AttrPartsList]
	if !ok || !v.El.IsContainer {
		return nil, false
	}
	var out []uint16
	for _, c := range v.Children {
		if c.El.Type >= tlv.TypeUnsignedInt1 && c.El.Type <= tlv.TypeUnsignedInt8 && c.El.Uint <= 0xFFFE {
			out = append(out, uint16(c.El.Uint))
		}
	}
	return out, true
}

// rootDeviceTypesOnly: the root Descriptor reported its DeviceTypeList and
// every entry is Root Node (an empty list included).
func rootDeviceTypesOnly(desc *Cluster) bool {
	v, ok := desc.cache.values[descriptor.AttrDeviceTypeList]
	if !ok || !v.El.IsContainer {
		return false
	}
	for _, e := range v.Children {
		for _, f := range e.Children {
			if f.El.Tag == tlv.ContextTag(0) && f.El.Uint != uint64(rootNodeDeviceType) {
				return false
			}
		}
	}
	return true
}

// Seeded reports whether the node's structure is known ([Hooks].Seeded;
// ClientNodeLifecycle.ts:51-57).
func (n *Node) Seeded() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.seeded
}

// icdSustainHooks gives the default subscription the ICD branches of the
// sustained subscription (ClientNodeInteraction.ts:174, :178).
func (n *Node) icdSustainHooks(req *SubscribePaths) {
	if n.icd != nil {
		req.Wakefulness = n.icd.Wakefulness
		req.PeerFed = n.icd.peerFed
	}
}
