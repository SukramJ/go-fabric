// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/controller/interaction"
)

// latchLabel sets the node's once-per-start latch of the fabric-label
// reconciliation; false when it was set already (NetworkClient.ts:269-273
// at 0ceb202d).
func (n *Node) latchLabel() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.labelReconciled {
		return false
	}
	n.labelReconciled = true
	return true
}

// reconcileLabel pushes our fabric label to the peer once per start, on
// the first SubscriptionAlive — NetworkClient.#reconcileFabricLabel
// (NetworkClient.ts:259-307): nothing without a peer address or an
// OperationalCredentials cluster in the image (:275-278) or without a
// label of our fabric (:280-288); the peer's entry for our fabric is the
// one of its CurrentFabricIndex — the device-side index, never our
// peerAddress.FabricIndex (:290-294); nothing when there is none or its
// label is ours (:295-297); else UpdateFabricLabel, a non-Ok status and an
// error each noted (:299-306). It is not retried within the start.
func (r *Registry) reconcileLabel(n *Node) {
	addr, ok := n.PeerAddress()
	oc := n.opCreds()
	if !ok || oc == nil {
		return
	}
	f := r.fabricByIndex(addr.FabricIndex)
	if f == nil || f.Label == "" {
		return
	}
	label := f.Label
	current := currentFabricIndex(oc)
	var own *fabricDescriptor
	for _, e := range peerFabrics(oc) {
		if e.FabricIndex == current {
			own = &e
			break
		}
	}
	if own == nil || own.Label == label {
		return
	}
	res, err := n.Invoke(r.ctx, opCredsCommand(opCredsCmdUpdateFabricLabel), updateFabricLabelRequest(label), interaction.InvokeOptions{})
	if err != nil {
		r.log.Info("matter.controller.client.registry.label_reconcile_failed", slog.String("node", n.ID()), slog.String("err", err.Error()))
		return
	}
	nr, err := nocResponseOf(res)
	if err != nil {
		r.log.Info("matter.controller.client.registry.label_not_reconciled", slog.String("node", n.ID()), slog.String("err", err.Error()))
		return
	}
	if nr.status != nocStatusOk {
		r.log.Info("matter.controller.client.registry.label_not_reconciled", slog.String("node", n.ID()), slog.Int("status", int(nr.status)))
	}
}
