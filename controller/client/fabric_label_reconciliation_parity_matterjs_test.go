// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/network/FabricLabelReconciliationTest.ts
// at 862ad75f.
//
// matter.js re-runs the once-per-start reconciliation by clearing
// NetworkClient's fabricLabelReconciled latch and emitting
// subscriptionAlive (reconcile :12-18); the Go latch is the node's per
// start (client.Node latchLabel), so the port starts the registry's node
// again, whose first SubscriptionAlive reconciles (Registry.reconcileLabel).
// The controller's fabric label is the commissioner fabric's Label, a
// field the registry's work reads; matter.js's controllerFabric.setLabel
// has no Go setter, so the label is set between a controller restart
// (which waits for the registry's work) and the node's start. The device's
// label is the one its store holds for the fabric.
package client_test

import (
	"testing"
)

// deviceFabricLabel is the device's label of its one fabric.
func (cp *clientPair) deviceFabricLabel(t *testing.T) string {
	t.Helper()
	fabrics, err := cp.pair.Device.Node().Fabrics(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("device fabrics %+v, %v", fabrics, err)
	}
	return fabrics[0].Label
}

// reconcile is reconcile (:12-18): a new start of the node, whose first
// SubscriptionAlive runs the reconciliation, settled; relabel, when not
// empty, is the controller's new fabric label (setLabel).
func (cp *clientPair) reconcile(t *testing.T, relabel string, until func() bool) {
	t.Helper()
	id := cp.node.ID()
	if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	if relabel != "" {
		cp.pair.Controller.Fabric().Label = relabel
	}
	cp.node = cp.pair.Controller.Registry().Get(id)
	if cp.node == nil {
		t.Fatal("the restarted registry does not know the node")
	}
	cp.run(t, cp.node.Start)
	cp.eventually(t, "the default subscription again", cp.node.SubscriptionActive)
	cp.eventually(t, "the reconciliation", until)
}

// TestFabricLabelReconciliationParityMatterJS ports the describe
// "FabricLabelReconciliation" (FabricLabelReconciliationTest.ts:20-52).
func TestFabricLabelReconciliationParityMatterJS(t *testing.T) {
	// :23-38.
	t.Run("pushes a drifted controller fabric label to the peer on start", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{})
		fabric := cp.pair.Controller.Fabric()
		if got := cp.deviceFabricLabel(t); got != fabric.Label {
			t.Fatalf("device label %q, controller label %q at the start", got, fabric.Label)
		}
		cp.reconcile(t, "Relabelled", func() bool { return cp.deviceFabricLabel(t) == "Relabelled" })
	})

	// :40-51.
	t.Run("leaves the peer untouched when the label already matches", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{})
		before := cp.deviceFabricLabel(t)
		cp.reconcile(t, "", func() bool { return true })
		cp.advance(t, 10*advanceStep)
		if got := cp.deviceFabricLabel(t); got != before {
			t.Errorf("device label %q, want %q untouched", got, before)
		}
	})
}
