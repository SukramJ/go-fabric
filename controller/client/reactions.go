// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// The registry's reactions to a peer's BasicInformation events —
// node/client/Peers.ts:714-893 at 0ceb202d. They hang on each node's
// Event hook (D6 of brief AL): matter.js registers them once per node when
// the BasicInformation client is installed (:719-729); the Go hook exists
// from the node's creation, so that guard has nothing to guard. Like
// matter.js's handlers, which nothing awaits (:727-729, :790), each runs
// apart from the subscription that delivered the event — a reaction that
// deletes the node waits for that subscription to end.

// onEvent dispatches a root BasicInformation event: StartUp, ShutDown,
// Leave (basicinformation.EventStartUp 0, EventShutDown 1, EventLeave 2).
// A Leave first reaches a decommission's latch
// (CommissioningClient.ts:383-391).
func (r *Registry) onEvent(n *Node, endpoint uint16, cluster, event uint32, value any) {
	if endpoint != 0 || cluster != basicinformation.ClusterID {
		return
	}
	switch event {
	case basicinformation.EventLeave:
		idx, ok := leaveFabricIndex(value)
		if !ok {
			return
		}
		r.noteLeave(n, idx)
		r.goWork(func() { r.onLeave(n, idx) })
	case basicinformation.EventShutDown:
		r.goWork(func() { r.onShutdown(n) })
	case basicinformation.EventStartUp:
		r.goWork(func() { r.onStartUp(n) })
	}
}

// goWork runs fn as background work of the registry, unless it is
// closed; Close waits for it.
func (r *Registry) goWork(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.work.Add(1)
	go func() {
		defer r.work.Done()
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

// onLeave is Peers.#onLeave (Peers.ts:785-835): under the mutex a closing
// node, or one under decommission, is left alone (:786-793); the peer's
// fabric entry of the event's index (:795-799), the node's peer address
// (:801-804) and our fabric for that entry, of the address's index
// (:806-810), must all exist; a leave without an active subscription may
// be stale and is ignored (:812-824); then the peer has left: the node is
// decommissioned (:826-827), its sessions given up (:828-832) and the node
// deleted (:833).
func (r *Registry) onLeave(n *Node, fabricIndex uint8) {
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
	if !n.SubscriptionActive() {
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

// onShutdown is Peers.#onShutdown (Peers.ts:837-858): a node that is
// closing or not started is left alone (:838-840), a shutdown without an
// active subscription ignored (:842-851); the device reboots, so every
// session with it is given up — SessionManager.handlePeerShutdown with
// asOf now (SessionManager.ts:624-631), which the Go HandlePeerLoss reads
// as a zero asOf: no session can be created after now.
func (r *Registry) onShutdown(n *Node) {
	if n.isClosing() || !n.isStarted() {
		return
	}
	if !n.SubscriptionActive() {
		r.log.Debug("matter.controller.client.registry.shutdown_ignored", slog.String("node", n.ID()))
		return
	}
	if addr, ok := n.PeerAddress(); ok {
		r.cfg.Exchanges.HandlePeerLoss(addr, ErrPeerShutdown, time.Time{})
	}
}

// onStartUp is Peers.#onStartUp (Peers.ts:860-893): with the same guards
// (:868-881) and a peer address (:883-886), the sessions created before
// the peer's most recently active one are given up — that one and newer
// ones are kept (:888-892, SessionManager.maybeSessionFor :589-606 is
// peer.Peer.NewestSession). Without a session matter.js cuts at now; the
// Go HandlePeerLoss reads a zero asOf as the same cut, on the exchange
// manager's own clock.
func (r *Registry) onStartUp(n *Node) {
	if n.isClosing() || !n.isStarted() {
		return
	}
	if !n.SubscriptionActive() {
		r.log.Debug("matter.controller.client.registry.startup_ignored", slog.String("node", n.ID()))
		return
	}
	addr, ok := n.PeerAddress()
	if !ok {
		return
	}
	var asOf time.Time
	if p := n.peer(); p != nil {
		if s := p.NewestSession(); s != nil {
			asOf = s.CreatedAt()
		}
	}
	r.cfg.Exchanges.HandlePeerLoss(addr, ErrPeerShutdown, asOf)
}
