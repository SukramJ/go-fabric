// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestGencomm_ArmOverADueWindowArmsNothingForAnEndedSession pins
// R2-P1-dr-sec-03: an arm that finds the window armed but past its
// deadline runs that window's rollback itself. When the rollback ends the
// arming session — every PASE session, closed by onFailSafeRolledBack (the
// bridge's PASE close), or a CASE session of the fabric the window's
// AddNOC added, which it removes — the arm leaves no fail-safe armed for
// nobody; an arm from a session the rollback leaves standing arms as
// before. The fake's wall step moves Now past the deadline without firing
// the watcher.
func TestGencomm_ArmOverADueWindowArmsNothingForAnEndedSession(t *testing.T) {
	t.Parallel()
	arm := func(ctx context.Context, r failSafeRig) error {
		_, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		return err
	}
	for _, c := range []struct {
		name string
		// arm runs the arm in the gap over the rig whose window AddNOC
		// re-stamped with fabric idx; wantErr: the wire arm's answer.
		arm     func(ctx context.Context, r failSafeRig, idx uint8) error
		wantErr bool
	}{
		{"PASE auto-arm", func(ctx context.Context, r failSafeRig, _ uint8) error {
			r.gc.AutoArmOnPaseEstablished(ctx)
			return nil
		}, false},
		{"ArmFailSafe over PASE", func(ctx context.Context, r failSafeRig, _ uint8) error {
			return arm(ctx, r)
		}, true},
		{"ArmFailSafe over CASE of the added fabric", func(ctx context.Context, r failSafeRig, idx uint8) error {
			return arm(im.WithFabricFilter(ctx, true, idx), r)
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
			st := memstore.New()
			r := newFailSafeRig(t, st, st, clk, nil)
			var pasesClosed atomic.Int32
			r.gc.SetOnFailSafeRolledBack(func(context.Context) { pasesClosed.Add(1) })
			idx := r.armAndAddNOC(ctx, t)
			defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()

			clk.StepWall(61 * time.Second)
			err := c.arm(ctx, r, idx)
			if c.wantErr && spec.StatusOf(err) != im.StatusFailure {
				t.Fatalf("the arm: status %v (%v), want %v", spec.StatusOf(err), err, im.StatusFailure)
			}
			if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
				t.Fatalf("%d fabric(s) survived an arm over the due window: its rollback never ran", len(fabrics))
			}
			if pasesClosed.Load() != 1 {
				t.Fatalf("the rollback closed the PASE sessions %d times, want 1", pasesClosed.Load())
			}
			if r.gc.FailSafeArmed() {
				t.Fatal("the arm left a fail-safe armed for a session its own rollback ended")
			}
		})
	}
}

// TestGencomm_ArmOverADueWindowFromASurvivingSessionArms is the control:
// a committed fabric's CASE arm over a due window that its rollback leaves
// standing — the window was armed over PASE and no AddNOC re-stamped it —
// arms a fresh fail-safe for that fabric.
func TestGencomm_ArmOverADueWindowFromASurvivingSessionArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	st := memstore.New()
	r := newFailSafeRig(t, st, st, clk, nil)
	r.gc.SetOnFailSafeRolledBack(func(context.Context) {})
	idx := r.armAndAddNOC(ctx, t)
	if !r.commissioningComplete(ctx, t, idx) {
		t.Fatal("CommissioningComplete failed")
	}
	defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe over PASE: %+v, %v", resp, err)
	}

	clk.StepWall(61 * time.Second)
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
	if err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe over CASE of the committed fabric: %+v, %v; want OK", resp, err)
	}
	if fabric, armed := r.gc.FailSafeFabric(); !armed || fabric != idx {
		t.Fatalf("FailSafeFabric = %d, %v; want %d, armed", fabric, armed, idx)
	}
}

// TestGencomm_CommissioningCompleteOnADueWindowCommitsNothing is
// R2-P2-comm-09 = N-core2-1: a CommissioningComplete that arrives once the
// window is past its deadline, before the watcher ran, answers
// NoFailSafe and the window's AddNOC is rolled back.
func TestGencomm_CommissioningCompleteOnADueWindowCommitsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	st := memstore.New()
	r := newFailSafeRig(t, st, st, clk, nil)
	idx := r.armAndAddNOC(ctx, t)
	defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()
	clk.StepWall(61 * time.Second)
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x04, nil)
	if err != nil {
		t.Fatalf("CommissioningComplete: %v", err)
	}
	if code := resp.(core.CommissioningCompleteResponse).ErrorCode; code != core.CommissioningErrorNoFailSafe {
		t.Fatalf("CommissioningComplete on a due window: ErrorCode %d, want NoFailSafe (%d)", code, core.CommissioningErrorNoFailSafe)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
		t.Fatalf("%d fabric(s) after a CommissioningComplete on a due window, want the AddNOC rolled back", len(fabrics))
	}
}
