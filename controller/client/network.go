// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"log/slog"
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

// dropDefaultSubscription closes the default subscription no longer
// desired (NetworkClient.ts:113-196).
func (n *Node) dropDefaultSubscription() {
	n.mu.Lock()
	s := n.sub
	n.sub, n.subOwner = nil, nil
	n.mu.Unlock()
	s.Close()
}

// syncAutoSubscribe is NetworkClient.#syncAutoSubscribe
// (NetworkClient.ts:113-196): a newly commissioned node reads the default
// subscription's paths once (:133-143, unless NoInitialRead); then, when a
// subscription is desired and none is active, the default subscription is
// established, sustained, with the event minimum past the highest event
// seen and a bootstrap read unless the initial read ran (:149-156).
func (n *Node) syncAutoSubscribe(ctx context.Context) error {
	n.subSync.Lock()
	defer n.subSync.Unlock()
	n.mu.Lock()
	desired := n.settings.autoSubscribe && !n.settings.disabled
	newly := n.newlyCommissioned
	n.newlyCommissioned = false
	req := n.defaultRequest()
	active := n.sub != nil
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
	if desired == active {
		return nil
	}
	if !desired {
		n.dropDefaultSubscription()
		return nil
	}
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
	alive := func() {
		n.mu.RLock()
		s := n.sub
		n.mu.RUnlock()
		n.noteICDActive()
		if s != nil && s.ID() != interaction.NoSubscription && n.cfg.Hooks.SubscriptionAlive != nil {
			n.cfg.Hooks.SubscriptionAlive()
		}
	}
	n.icdSustainHooks(&sreq)
	sreq.Updated = func(*Report) { alive() }
	sreq.KeepaliveReceived = alive
	sreq.Closed = func() {
		n.mu.Lock()
		if n.subOwner == owner {
			n.sub, n.subOwner = nil, nil
		}
		n.mu.Unlock()
	}
	sreq.ActiveChanged = func(active bool) {
		if h := n.cfg.Hooks.SubscriptionStatusChanged; h != nil {
			h(active)
		}
		if n.icd != nil {
			n.icd.onSubscriptionActive(active)
		}
		if active {
			if h := n.cfg.Hooks.SubscriptionAlive; h != nil {
				h()
			}
			if n.cfg.FlushInterval > 0 {
				n.persist.flush()
			}
		}
		n.recompute()
	}
	n.mu.Lock()
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
	n.adoptDefaultSubscription(s, &sub)
	return nil
}

// subscriptionOwner is the identity of one default subscription; not
// zero-sized, so two owners never share an address.
type subscriptionOwner struct{ _ byte }

// adoptDefaultSubscription makes s the default subscription, also in the
// caller's own variable its callbacks compare against. s can go
// active before n.sub names it, and an ICD trigger that ran then read
// SubscriptionActive false: the ICD client gets the edge again now that it
// can see it (a duplicate trigger is absorbed by its own guards).
func (n *Node) adoptDefaultSubscription(s *Subscription, own **Subscription) {
	n.mu.Lock()
	*own, n.sub = s, s
	n.mu.Unlock()
	if s.Active() && n.icd != nil {
		n.icd.onSubscriptionActive(true)
	}
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
		DataVersionFilters: n.s.injectVersionFilters(sc, false),
	}
	n.mu.RUnlock()
	ia, err := n.interaction()
	if err != nil {
		return err
	}
	rep, err := ia.ReadWith(ctx, r)
	if err != nil {
		return err
	}
	n.apply(sc, rep, emitEvents)
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

// observePeer binds the peer's signals (NetworkClient.ts:79-83): an
// unresponsive establishment sets the likely-offline latch (:244-247), a
// new session clears it (:254-257), each recomputes the connection state.
func (n *Node) observePeer() func() {
	p := n.peer()
	if p == nil {
		return nil
	}
	return p.Observe(peer.Observer{
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
}

// recompute sets the connection state from the liveness signals —
// NetworkClient.#recomputeConnectionState (NetworkClient.ts:215-238): an
// active subscription with a session of the peer is Connected (:223), a
// disabled or stopped node Disconnected (:226-230), the latch
// WaitingForDeviceDiscovery (:231-232), anything else Reconnecting; a
// transition calls ConnectionStateChanged (ClientNodeLifecycle.ts:101-107).
func (n *Node) recompute() {
	n.mu.Lock()
	active := n.sub != nil && n.sub.Active()
	hasSession := n.cfg.Peer == nil || len(n.cfg.Peer.Sessions()) > 0
	st := connectionStateFor(active, hasSession, n.settings.disabled || !n.started, n.likelyOffline)
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

// connectionStateFor is the decision of NetworkClient.ts:223-235.
func connectionStateFor(active, hasSession, offline, likelyOffline bool) ConnectionState {
	switch {
	case active && hasSession:
		return Connected
	case offline:
		return Disconnected
	case likelyOffline:
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
