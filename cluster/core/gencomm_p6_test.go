// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// ArmFailSafe with ExpiryLengthSeconds 0 on a fail-safe that is not armed
// answers OK and changes nothing, the DataVersion included; the disarm of
// an armed one resets the Breadcrumb and advances it (review II,
// R2-P6-par-1020).
func TestDisarmOfAnUnarmedFailSafeKeepsTheDataVersion(t *testing.T) {
	t.Parallel()
	gc := newGencomm(t, core.GeneralCommissioningConfig{LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600})
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	v0 := gc.MatterDataVersion()
	resp, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 0, Breadcrumb: 4})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := resp.(core.ArmFailSafeResponse); !ok || r.ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(0) not armed = %#v, want OK", resp)
	}
	if v := gc.MatterDataVersion(); v != v0 {
		t.Errorf("DataVersion %d -> %d on a disarm of a fail-safe that was not armed", v0, v)
	}
	if _, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 4}); err != nil {
		t.Fatal(err)
	}
	armed := gc.MatterDataVersion()
	if _, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 0}); err != nil {
		t.Fatal(err)
	}
	if v := gc.MatterDataVersion(); v == armed {
		t.Errorf("DataVersion %d unchanged after the disarm reset the Breadcrumb 4 -> 0", v)
	}
}
