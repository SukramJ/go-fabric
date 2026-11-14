// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestGencomm_ArmThatWaitedForTheRollbackArmsNothingForAnEndedSession pins
// review II R2-P1-rw-05: the watcher expires the window and runs its
// rollback; an arm that arrives during that rollback waits for it (core2
// item 1b) rather than running it itself. When that rollback ends the
// arming session — every PASE session, closed by onFailSafeRolledBack, or
// a CASE session of the fabric the window's AddNOC added, which it
// removes — the arm arms nothing, as an arm over a due window it rolls
// back itself does (R2-P1-dr-sec-03). The host hook holds the watcher's
// rollback; synctest.Wait lets the arm run as far as it gets.
func TestGencomm_ArmThatWaitedForTheRollbackArmsNothingForAnEndedSession(t *testing.T) {
	t.Parallel()
	wireArm := func(ctx context.Context, r failSafeRig) error {
		_, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		return err
	}
	for _, c := range []struct {
		name    string
		arm     func(ctx context.Context, r failSafeRig, idx uint8) error
		wantErr bool
	}{
		{"PASE auto-arm", func(ctx context.Context, r failSafeRig, _ uint8) error {
			r.gc.AutoArmOnPaseEstablished(ctx)
			return nil
		}, false},
		{"ArmFailSafe over PASE", func(ctx context.Context, r failSafeRig, _ uint8) error {
			return wireArm(ctx, r)
		}, true},
		{"ArmFailSafe over CASE of the added fabric", func(ctx context.Context, r failSafeRig, idx uint8) error {
			return wireArm(im.WithFabricFilter(ctx, true, idx), r)
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
				st := memstore.New()
				atRollback, release := make(chan struct{}), make(chan struct{})
				r := newFailSafeRig(t, st, st, clk, func(oc *core.OperationalCredentials) func(context.Context, uint8) {
					return func(ctx context.Context, fi uint8) {
						close(atRollback)
						<-release
						oc.OnFailSafeExpiry(ctx, fi)
					}
				})
				var pasesClosed atomic.Int32
				r.gc.SetOnFailSafeRolledBack(func(context.Context) { pasesClosed.Add(1) })
				idx := r.armAndAddNOC(ctx, t)
				defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()

				clk.Advance(61 * time.Second)
				<-atRollback
				var armErr error
				armed := make(chan struct{})
				go func() {
					defer close(armed)
					armErr = c.arm(ctx, r, idx)
				}()
				synctest.Wait()
				close(release)
				<-armed
				synctest.Wait()
				if c.wantErr && spec.StatusOf(armErr) != im.StatusFailure {
					t.Fatalf("the arm: status %v (%v), want %v", spec.StatusOf(armErr), armErr, im.StatusFailure)
				}
				if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
					t.Fatalf("%d fabric(s) survived the expiry", len(fabrics))
				}
				if pasesClosed.Load() != 1 {
					t.Fatalf("the rollback closed the PASE sessions %d times, want 1", pasesClosed.Load())
				}
				if r.gc.FailSafeArmed() {
					t.Fatal("the arm that waited for the rollback left a fail-safe armed for a session that rollback ended")
				}
			})
		})
	}
}

// TestGencomm_ArmThatWaitedForTheRollbackFromASurvivingSessionArms is the
// control: a committed fabric's CASE arm waits for the rollback of a
// window armed over PASE that no AddNOC re-stamped; that rollback leaves
// the CASE session standing, so the arm arms a fresh fail-safe for it.
func TestGencomm_ArmThatWaitedForTheRollbackFromASurvivingSessionArms(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
		st := memstore.New()
		var hold atomic.Bool
		atRollback, release := make(chan struct{}), make(chan struct{})
		r := newFailSafeRig(t, st, st, clk, func(oc *core.OperationalCredentials) func(context.Context, uint8) {
			return func(ctx context.Context, fi uint8) {
				if hold.Load() {
					close(atRollback)
					<-release
				}
				oc.OnFailSafeExpiry(ctx, fi)
			}
		})
		r.gc.SetOnFailSafeRolledBack(func(context.Context) {})
		idx := r.armAndAddNOC(ctx, t)
		if !r.commissioningComplete(ctx, t, idx) {
			t.Fatal("CommissioningComplete failed")
		}
		defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()
		if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe over PASE: %+v, %v", resp, err)
		}
		hold.Store(true)

		clk.Advance(61 * time.Second)
		<-atRollback
		var resp any
		var armErr error
		armed := make(chan struct{})
		go func() {
			defer close(armed)
			resp, armErr = r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		}()
		synctest.Wait()
		close(release)
		<-armed
		if armErr != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe over CASE of the committed fabric: %+v, %v; want OK", resp, armErr)
		}
		if fabric, armed := r.gc.FailSafeFabric(); !armed || fabric != idx {
			t.Fatalf("FailSafeFabric = %d, %v; want %d, armed", fabric, armed, idx)
		}
	})
}
