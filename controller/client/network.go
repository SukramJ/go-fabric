// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"log/slog"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
)

// defaultRequest is the default subscription — every attribute, every
// event urgent, fabric-filtered, replacing our other subscriptions —
// narrowed by the stored options (NetworkClient.ts:121-128 at 0ceb202d).
// The caller holds the lock.
func (n *Node) defaultRequest() interaction.SubscribeRequest {
	r := interaction.SubscribeRequest{
		Attributes:           []im.ConcreteAttributePath{{}},
		Events:               []im.ConcreteEventPath{{IsUrgent: true}},
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

// syncAutoSubscribe is NetworkClient.#syncAutoSubscribe
// (NetworkClient.ts:113-196): a newly commissioned node reads the default
// subscription's paths once (:133-143, unless NoInitialRead); then, when a
// subscription is desired and none is active, the default subscription is
// established, sustained, with the event minimum past the highest event
// seen and a bootstrap read unless the initial read ran (:149-156).
func (n *Node) syncAutoSubscribe(ctx context.Context) error {
	n.mu.Lock()
	desired := n.settings.autoSubscribe && !n.settings.disabled
	newly := n.newlyCommissioned
	n.newlyCommissioned = false
	req := n.defaultRequest()
	active := n.sub != nil
	n.mu.Unlock()
	didRead := false
	if newly && !n.cfg.NoInitialRead {
		if _, err := n.Read(ctx, ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: req.Attributes, Events: req.Events, NoFabricFilter: req.NoFabricFilter}}); err != nil {
			return err
		}
		didRead = true
	}
	if desired == active {
		return nil
	}
	if !desired {
		n.mu.Lock()
		s := n.sub
		n.sub = nil
		n.mu.Unlock()
		s.Close()
		return nil
	}
	sreq := req
	sreq.Sustain = true
	n.mu.RLock()
	sreq.EventFilters = []im.EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}}
	n.mu.RUnlock()
	if !didRead {
		sreq.BootstrapRead = func(ctx context.Context) error { return n.bootstrapRead(ctx, req) }
	}
	var sub *interaction.Subscription
	alive := func() {
		n.mu.RLock()
		s := n.sub
		n.mu.RUnlock()
		if s != nil && s.ID() != interaction.NoSubscription && n.cfg.Hooks.SubscriptionAlive != nil {
			n.cfg.Hooks.SubscriptionAlive()
		}
	}
	sreq.Updated = func(*interaction.Report) { alive() }
	sreq.KeepaliveReceived = alive
	sreq.Closed = func() {
		n.mu.Lock()
		if n.sub == sub {
			n.sub = nil
		}
		n.mu.Unlock()
	}
	sreq.ActiveChanged = func(active bool) {
		if h := n.cfg.Hooks.SubscriptionStatusChanged; h != nil {
			h(active)
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
	s, err := n.Subscribe(ctx, sreq)
	if err != nil {
		return err
	}
	n.mu.Lock()
	sub, n.sub = s, s
	n.mu.Unlock()
	return nil
}

// bootstrapRead is the read a sustained default subscription performs
// before its first attempt — SustainedSubscription.ts:187-204 reading the
// subscription's request, its version filters and event minimum, and
// handing the result to the request's updated (here: the image).
func (n *Node) bootstrapRead(ctx context.Context, req interaction.SubscribeRequest) error {
	sc := newScope(req.Attributes, req.NoFabricFilter)
	n.mu.RLock()
	r := interaction.ReadRequest{
		Attributes: req.Attributes, Events: req.Events, NoFabricFilter: req.NoFabricFilter,
		EventFilters:       []im.EventMinimumNumber{{EventMin: n.settings.maxEventNumber + 1}},
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
	n.apply(sc, rep)
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
	n.mu.Unlock()
	if changed && n.cfg.Hooks.ConnectionStateChanged != nil {
		n.cfg.Hooks.ConnectionStateChanged(st)
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
func (n *Node) deliverEvent(e *interaction.EventData) {
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
			n.log.Warn("matter.controller.client.event_undecodable", slog.Uint64("cluster", uint64(path.Cluster)), slog.Uint64("event", uint64(path.Event)), slog.String("err", err.Error()))
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

// evaluateSeeded marks the node seeded once its root has BasicInformation
// and the image more than the root endpoint (Peers.ts:747-759,
// ClientNodeLifecycle.ts:51-75).
func (n *Node) evaluateSeeded() {
	n.mu.Lock()
	if n.seeded || readyCluster(n.s.root, basicinformation.ClusterID) == nil {
		n.mu.Unlock()
		return
	}
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
	n.seeded = true
	n.mu.Unlock()
	if h := n.cfg.Hooks.Seeded; h != nil {
		h()
	}
}

// Seeded reports whether the image has been read at least once: the root
// has BasicInformation and there is more than the root endpoint
// (ClientNodeLifecycle.ts:51-57).
func (n *Node) Seeded() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.seeded
}
