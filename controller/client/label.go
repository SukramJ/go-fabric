// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// nocStatusLabelConflict is NodeOperationalCertStatusEnum LabelConflict
// (parity/schema.json, OperationalCredentials).
const nocStatusLabelConflict uint8 = 10

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
	if err := r.pushLabel(r.ctx, n); err != nil {
		var se *nocStatusError
		if errors.As(err, &se) {
			r.log.Info("matter.controller.client.registry.label_not_reconciled", slog.String("node", n.ID()), slog.Int("status", int(se.status)))
			return
		}
		r.log.Info("matter.controller.client.registry.label_reconcile_failed", slog.String("node", n.ID()), slog.Any("err", err))
	}
}

// nocStatusError is an UpdateFabricLabel answered with a NOCResponse
// status other than Ok.
type nocStatusError struct{ status uint8 }

func (e *nocStatusError) Error() string {
	if e.status == nocStatusLabelConflict {
		return "client: cannot set the fabric label because another fabric on this device already uses this name"
	}
	return fmt.Sprintf("client: UpdateFabricLabel answered NOCResponse status %d", e.status)
}

// Unwrap is commissioner.ErrFabricLabelConflict for LabelConflict.
func (e *nocStatusError) Unwrap() error {
	if e.status == nocStatusLabelConflict {
		return commissioner.ErrFabricLabelConflict
	}
	return nil
}

// pushLabel is the reconciliation's work for n: nothing to do is nil; an
// UpdateFabricLabel that fails is the error — a LabelConflict status
// commissioner.ErrFabricLabelConflict (CommissioningController.ts:972-976
// at 862ad75f, FabricLabelConflictError), another one a *nocStatusError.
func (r *Registry) pushLabel(ctx context.Context, n *Node) error {
	addr, ok := n.PeerAddress()
	oc := n.opCreds()
	if !ok || oc == nil {
		return nil
	}
	f := r.fabricByIndex(addr.FabricIndex)
	if f == nil {
		return nil
	}
	label := f.CurrentLabel()
	if label == "" {
		return nil
	}
	current := currentFabricIndex(oc)
	var own *fabricDescriptor
	for _, e := range peerFabrics(oc) {
		if e.FabricIndex == current {
			own = &e
			break
		}
	}
	if own == nil || own.Label == label {
		return nil
	}
	res, err := n.Invoke(ctx, opCredsCommand(opCredsCmdUpdateFabricLabel), updateFabricLabelRequest(label), InvokeOptions{})
	if err != nil {
		return err
	}
	nr, err := nocResponseOf(res)
	if err != nil {
		return err
	}
	if nr.status == nocStatusOk {
		return nil
	}
	return &nocStatusError{status: nr.status}
}

// SetFabricLabel sets the label of our fabrics, persists it with each
// fabric's record (commissioner.Fabric.SetLabel), and reconciles it to
// every commissioned node that is connected; a node that is not gets it at
// its next start, when the reconciliation runs again on its first
// SubscriptionAlive — matter.js CommissioningController.updateFabricLabel
// (packages/matter.js/src/CommissioningController.ts:986-1033 at
// 862ad75f): the fabric's setLabel (MatterController.ts:796-798), then
// validateAndUpdateFabricLabel for each connected node (:941-980), an
// offline node updated on its next connection. matter.js returns after
// the first connected node it updated (:996-998); every connected node
// gets the label here. A label longer than 32 characters is
// commissioner.ErrLabel; the error of each node it could not update is
// joined into the returned error, a refusal of the name
// commissioner.ErrFabricLabelConflict.
func (r *Registry) SetFabricLabel(ctx context.Context, label string) error {
	for _, f := range r.cfg.Fabrics {
		if err := f.SetLabel(label); err != nil {
			return err
		}
	}
	var errs []error
	for _, n := range r.Commissioned() {
		if n.ConnectionState() != Connected {
			continue
		}
		if err := r.pushLabel(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("node %s: %w", n.ID(), err))
		}
	}
	return errors.Join(errs...)
}
