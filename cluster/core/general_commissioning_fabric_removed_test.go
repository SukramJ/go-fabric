// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestGencomm_FailSafeOfARemovedFabricExpires is R2-P3-admin-06: a
// fail-safe armed by a fabric does not stay armed for that fabric's index
// once the fabric is removed — it expires with its rollback, as its
// timeout does; another fabric's removal leaves it alone.
func TestGencomm_FailSafeOfARemovedFabricExpires(t *testing.T) {
	t.Parallel()
	gc := defaultGencomm(t)
	var expired []uint8
	gc.SetOnFailSafeExpired(func(_ context.Context, fabricIndex uint8) { expired = append(expired, fabricIndex) })
	ctx := context.Background()
	if resp, err := gc.MatterInvoke(im.WithFabricFilter(ctx, true, 2), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 900, Breadcrumb: 7}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe by fabric 2: %+v, %v", resp, err)
	}

	gc.FabricRemoved(ctx, 3)
	if fabric, armed := gc.FailSafeFabric(); !armed || fabric != 2 {
		t.Fatalf("after fabric 3's removal the fail-safe reads fabric %d armed %v, want fabric 2's armed", fabric, armed)
	}

	gc.FabricRemoved(ctx, 2)
	if fabric, armed := gc.FailSafeFabric(); armed {
		t.Fatalf("after fabric 2's removal the fail-safe is still armed for fabric %d", fabric)
	}
	if len(expired) != 1 || expired[0] != 2 {
		t.Errorf("expiry hook calls %v, want one for fabric 2 (the rollback the timeout runs)", expired)
	}
	if v, _ := gc.MatterRead(0x0000); v.(uint64) != 0 {
		t.Errorf("Breadcrumb %v after the expiry, want 0", v)
	}
	if resp, err := gc.MatterInvoke(im.WithFabricFilter(ctx, true, 1), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe by fabric 1 after fabric 2's removal: %+v, %v", resp, err)
	}
}
