// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
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

// ArmFailSafeFor refuses a fail-safe another fabric holds and leaves it
// as it is (review II, R2-P6-fsm-1017).
func TestArmFailSafeForRefusesAnotherFabricsWindow(t *testing.T) {
	t.Parallel()
	gc := newGencomm(t, core.GeneralCommissioningConfig{LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600})
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	if _, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	if err := gc.ArmFailSafeFor(context.Background(), 60, 2); err == nil {
		t.Error("ArmFailSafeFor for fabric 2 over fabric 1's fail-safe succeeded")
	}
	if fabric, armed := gc.FailSafeFabric(); !armed || fabric != 1 {
		t.Errorf("fail-safe after the refused arm = fabric %d, armed %v; want fabric 1, armed", fabric, armed)
	}
	if err := gc.ArmFailSafeFor(context.Background(), 60, 1); err != nil {
		t.Errorf("ArmFailSafeFor for the armed fabric: %v", err)
	}
}

// ArmFailSafeFor's fresh arm keeps a Breadcrumb written while the
// fail-safe was disarmed, as the PASE auto-arm does (review II,
// N-findev-4).
func TestArmFailSafeForKeepsTheBreadcrumb(t *testing.T) {
	t.Parallel()
	gc := newGencomm(t, core.GeneralCommissioningConfig{LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600})
	if err := gc.MatterWrite(context.Background(), 0x0000, uint64(5)); err != nil {
		t.Fatal(err)
	}
	if err := gc.ArmFailSafeFor(context.Background(), 60, 0); err != nil {
		t.Fatal(err)
	}
	if v, _ := gc.MatterRead(0x0000); v != uint64(5) {
		t.Errorf("Breadcrumb after ArmFailSafeFor's fresh arm = %v, want the written 5", v)
	}
}

// SetCurrentFabric (AddNOC's re-stamp) leaves a fail-safe that is not armed
// alone: a later arm does not inherit a stale added fabric (review II,
// R2-P6-fsm-1024).
func TestSetCurrentFabricWhileDisarmedStampsNothing(t *testing.T) {
	t.Parallel()
	var got core.FailSafeExpiry
	gc := newGencomm(t, core.GeneralCommissioningConfig{LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600})
	gc.SetOnFailSafeExpiry(func(_ context.Context, e core.FailSafeExpiry) { got = e })
	gc.SetCurrentFabric(3)
	if fabric, armed := gc.FailSafeFabric(); armed || fabric != 0 {
		t.Errorf("FailSafeFabric after a re-stamp while disarmed = %d, %v; want 0, false", fabric, armed)
	}
	if err := gc.ArmFailSafeFor(context.Background(), 60, 0); err != nil {
		t.Fatal(err)
	}
	gc.ExpireFailSafe(context.Background())
	if got.AddedFabric != 0 {
		t.Errorf("expiry reports AddedFabric %d for a window no AddNOC re-stamped", got.AddedFabric)
	}
}

// LastFabricRemoved whose delete of the stored acceptance fails keeps the
// acceptance in force — the one a restart would restore — and returns the
// error (review II, R2-P6-err-1023).
func TestLastFabricRemovedKeepsTheAcceptanceWhenTheDeleteFails(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	ctx := caseCtx()
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("acceptance: %d", code)
	}
	st.mu.Lock()
	st.fail = errors.New("disk")
	st.mu.Unlock()
	if err := gc.LastFabricRemoved(ctx); err == nil {
		t.Fatal("LastFabricRemoved over a failing store returned nil")
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 1 {
		t.Errorf("TcAcceptedVersion after the failed reset = %d, want the stored 1", got)
	}
}
