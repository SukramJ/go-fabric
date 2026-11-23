// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"testing"
	"time"
)

// TestAwaitDeviceSessionMatchesTheControllersFabric (review II,
// R2-C2-test-2503): the wait Commission and CommissionNode end with
// matches the device's CASE session on the controller's own fabric. Two
// controllers with the same node id (the default) and one device
// commissioned onto the first with an explicit NodeID: the second
// controller's wait for that node id is not satisfied by the first
// fabric's session, and is once the second controller commissioned the
// device too.
func TestAwaitDeviceSessionMatchesTheControllersFabric(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{})
	dev := site.AddDevice(Topology{})
	ca := site.AddController(ControllerOptions{FabricID: 0xA, Label: "fabric A"})
	cb := site.AddController(ControllerOptions{FabricID: 0xB, Label: "fabric B"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := ca.Commission(ctx, dev, CommissionOptions{NodeID: 5}); err != nil {
		t.Fatal(err)
	}
	short, cancelShort := context.WithTimeout(ctx, time.Second)
	defer cancelShort()
	if err := cb.awaitDeviceSession(short, dev, 5); err == nil {
		t.Fatal("controller B's wait was satisfied by the session on controller A's fabric")
	}
	if err := dev.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := cb.Commission(ctx, dev, CommissionOptions{NodeID: 5}); err != nil {
		t.Fatal(err)
	}
	if err := cb.awaitDeviceSession(ctx, dev, 5); err != nil {
		t.Fatalf("controller B's wait after its own commissioning: %v", err)
	}
}
