// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/controller/commissioner"
	"thingfabric.dev/go-fabric/internal/inflight"
)

// The registry's reactions to a peer's BasicInformation events —
// node/client/Peers.ts:772-948 at db91886c. They hang on each node's
// Event hook (D6 of brief AL): matter.js registers them once per node when
// the BasicInformation client is installed (:772-774); the Go hook exists
// from the node's creation, so that guard has nothing to guard. Each runs
// on a goroutine of its own, apart from the subscription that delivered
// the event — a reaction that deletes the node waits for that subscription
// to end. In matter.js only the Leave reaction runs apart: #onLeave's work
// is a Peers.#mutex task nothing awaits (:847). #onShutdown and #onStartUp
// are async and their promises are awaited by the emit
// (general/src/util/Observable.ts:359-402) and so by the report's processing
// (ClientEventEmitter.ts:56-60, ClientStructure.ts:1844); their guards
// (:901, :931) run before the subscription can become active
// (SustainedSubscription.ts:224, :282), so for an event of the priming
// report they see it inactive by construction. The Go reactions get that
// by the report's live flag: each acts only when the subscription was
// active both when the report arrived and when the reaction runs.

// onEvent dispatches a root BasicInformation event: StartUp, ShutDown,
// Leave (basicinformation.EventStartUp 0, EventShutDown 1, EventLeave 2).
// A Leave first reaches a decommission's latch
// (CommissioningClient.ts:426-437 at b25ed960). live is whether the
// default subscription was active when the report carrying the event
// arrived (Node.deliveryLive).
func (r *Registry) onEvent(n *Node, endpoint uint16, cluster, event uint32, value any, live bool) {
	if endpoint != 0 || cluster != basicinformation.ClusterID {
		return
	}
	switch event {
	case basicinformation.EventLeave:
		idx, ok := leaveFabricIndex(value)
		if !ok {
			return
		}
		r.noteLeave(n, idx, live)
		r.goReaction(n, event, func() { r.onLeave(n, idx, live) })
	case basicinformation.EventShutDown:
		r.goReaction(n, event, func() { r.onShutdown(n, live) })
	case basicinformation.EventStartUp:
		r.goReaction(n, event, func() { r.onStartUp(n, live) })
	}
}

// goReaction runs a reaction to n's event as background work (goWork),
// behind the test seam reactionGate when one is set.
func (r *Registry) goReaction(n *Node, event uint32, fn func()) {
	r.mu.Lock()
	gate := r.reactionGate
	r.mu.Unlock()
	r.goWork(func() {
		if gate != nil {
			gate(n, event)
		}
		fn()
	})
}

// goWork runs fn as background work of the registry, unless it is
// closed; Close waits for it. The work is counted in the clock's
// in-flight group (inflight.RegistryReactions) from here until fn
// returned, so a test site holds its clock for it.
func (r *Registry) goWork(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.work.Add(1)
	r.inflight.Enter(inflight.RegistryReactions)
	go func() {
		defer r.work.Done()
		defer r.inflight.Leave(inflight.RegistryReactions)
		fn()
	}()
}

// leaveFabricIndex is the fabric index of a Leave event's payload.
func leaveFabricIndex(value any) (uint8, bool) {
	switch v := value.(type) {
	case basicinformation.LeaveEvent:
		return v.FabricIndex, true
	case *basicinformation.LeaveEvent:
		if v != nil {
			return v.FabricIndex, true
		}
	}
	return 0, false
}

func (n *Node) isStarted() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.started
}

// fabricForDescriptor is FabricManager.forDescriptor (D5 of brief AL):
// our fabric with the descriptor's root public key and fabric id.
func (r *Registry) fabricForDescriptor(f fabricDescriptor) *commissioner.Fabric {
	for _, ours := range r.cfg.Fabrics {
		if sameFabric(ours.RootPublicKey, ours.FabricID, f) {
			return ours
		}
	}
	return nil
}

// onLeave is Peers.#onLeave (Peers.ts:842-892 at b25ed960): under the
// mutex a closing node, or one under decommission, is left alone
// (:843-850); the peer's fabric entry of the event's index (:852-856), the
// node's peer address (:858-861) and our fabric for that entry, of the
// address's index (:863-867), must all exist; a leave without an active
// subscription may be stale and is ignored (:869-881); then the peer has
// left: the node is decommissioned (:883-884), its sessions given up
// (:885-889) and the node deleted (:890).
//
// live is whether the subscription was active when the event's report
// arrived. The reaction runs on a goroutine of its own, after the report's
// hooks ran on the node's job queue, so a Leave of the priming report can
// reach the check below after the subscription became established; read
// then, the subscription is active and the stale Leave would delete the
// node it was just commissioned as (issue #628). The leave counts only
// when the subscription was active at both points — the rule matter.js
// states (:869-873), independent of scheduling; notes/parity/by_design.md
// records the difference.
func (r *Registry) onLeave(n *Node, fabricIndex uint8, live bool) {
	if n.isClosing() {
		return
	}
	r.op.Lock()
	defer r.op.Unlock()
	if n.isClosing() {
		return
	}
	if op := r.operationOf(n); op != nil && op.kind == opDecommission {
		return
	}
	var entry *fabricDescriptor
	for _, f := range peerFabrics(n.opCreds()) {
		if f.FabricIndex == fabricIndex {
			entry = &f
			break
		}
	}
	if entry == nil {
		return
	}
	addr, ok := n.PeerAddress()
	if !ok {
		return
	}
	if local := r.fabricForDescriptor(*entry); local == nil || local.FabricIndex != addr.FabricIndex {
		return
	}
	if !live || !n.SubscriptionActive() {
		r.log.Info("matter.controller.client.registry.leave_ignored", slog.String("node", n.ID()))
		return
	}
	r.log.Info("matter.controller.client.registry.peer_left", slog.String("node", n.ID()), slog.String("peer", peerAddressString(addr)))
	if n.icd != nil {
		n.icd.onDecommissioned()
	}
	if h := n.cfg.Hooks.Decommissioned; h != nil {
		h()
	}
	if p := n.peer(); p != nil {
		p.Disconnect(r.ctx, ErrPeerLeft)
	}
	if err := r.deleteNode(r.ctx, n, nil); err != nil {
		r.log.Warn("matter.controller.client.registry.delete_failed", slog.String("node", n.ID()), slog.Any("err", err))
	}
}

// onShutdown is Peers.#onShutdown (Peers.ts:894-915 at db91886c): a node
// that is closing or not started is left alone, a shutdown without an
// active subscription ignored; the device reboots, so every session with
// it is given up — SessionManager.handlePeerShutdown without a session to
// keep (Peers.ts:913, SessionManager.ts:722-735), every session registered
// now. live is onLeave's: the shutdown counts only when the subscription
// was active when its report arrived and is active now.
func (r *Registry) onShutdown(n *Node, live bool) {
	if n.isClosing() || !n.isStarted() {
		return
	}
	if !live || !n.SubscriptionActive() {
		r.log.Debug("matter.controller.client.registry.shutdown_ignored", slog.String("node", n.ID()))
		return
	}
	if addr, ok := n.PeerAddress(); ok {
		r.cfg.Exchanges.HandlePeerLossBefore(addr, ErrPeerShutdown, nil)
	}
}

// onStartUp is Peers.#onStartUp (Peers.ts:921-948 at b25ed960): with the
// same guards and a peer address, the sessions registered before the
// peer's most recently active one are given up — that one and every
// session registered after it are kept, whatever their creation times
// (:945-947, SessionManager.handlePeerShutdown with that session,
// SessionManager.ts:722-735; SessionManager.maybeSessionFor :683-700 is
// peer.Peer.NewestSession). Without a session every session registered
// now is given up. live is onLeave's, as for onShutdown.
func (r *Registry) onStartUp(n *Node, live bool) {
	if n.isClosing() || !n.isStarted() {
		return
	}
	if !live || !n.SubscriptionActive() {
		r.log.Debug("matter.controller.client.registry.startup_ignored", slog.String("node", n.ID()))
		return
	}
	addr, ok := n.PeerAddress()
	if !ok {
		return
	}
	var keep *exchange.Session
	if p := n.peer(); p != nil {
		keep = p.NewestSession()
	}
	r.cfg.Exchanges.HandlePeerLossBefore(addr, ErrPeerShutdown, keep)
}
