// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestClosedGeneralCommissioningArmsNothing (facade residual 2a): once
// Close ran — the node stops — no arm takes the fail-safe again: an
// ArmFailSafe over CASE or PASE fails and the internal arms refuse, while
// a disarm still answers. The node closes the server before it expires the
// fail-safe at its stop (node.stopParts).
func TestClosedGeneralCommissioningArmsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gc := defaultGencomm(t)
	if err := gc.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]context.Context{"CASE": im.WithFabricFilter(ctx, false, 2), "PASE": ctx} {
		// FAILURE: the error names no other status (this module's rule
		// for a stopping node; no reference read for it).
		if _, err := gc.MatterInvoke(c, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err == nil || errors.As(err, new(im.StatusCodeError)) {
			t.Errorf("ArmFailSafe over %s on a closed server: %v, want FAILURE", name, err)
		}
	}
	if err := gc.ArmFailSafeFor(ctx, 60, 0); err == nil {
		t.Error("ArmFailSafeFor armed a closed server")
	}
	if err := gc.ArmOnPaseEstablished(ctx); err == nil {
		t.Error("ArmOnPaseEstablished armed a closed server")
	}
	if gc.FailSafeArmed() {
		t.Error("a closed server's fail-safe is armed")
	}
	if _, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{}); err != nil {
		t.Errorf("a disarm on a closed server: %v", err)
	}
}
