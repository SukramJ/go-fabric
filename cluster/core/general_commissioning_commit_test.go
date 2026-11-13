// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// pendingNOCKey is the settings key of OperationalCredentials' durable
// pending-NOC marker (operational_credentials_pending.go).
const pendingNOCKey = "operational_credentials.pending_noc"

// clearFailingStore is a memstore whose clear of the pending-NOC marker
// fails while fail is set — exactly that write, every other one lands.
type clearFailingStore struct {
	*memstore.Store
	fail atomic.Bool
}

func (s *clearFailingStore) SetSetting(ctx context.Context, key, value string) error {
	if key == pendingNOCKey && value == "" && s.fail.Load() {
		return errCrashed
	}
	return s.Store.SetSetting(ctx, key, value)
}

// failSafeRig is the node's wiring of the two clusters (node/root.go):
// one store for both, AddNOC re-stamping the fail-safe, a fresh arm and
// CommissioningComplete clearing OperationalCredentials' pending state,
// the expiry rolling it back. expired, when set, runs in place of
// OnFailSafeExpiry and calls it itself.
type failSafeRig struct {
	gc *core.GeneralCommissioning
	oc *core.OperationalCredentials
}

func newFailSafeRig(t *testing.T, st core.StoreFacade, settings mstore.Settings, clk clock.Clock,
	expired func(oc *core.OperationalCredentials) func(context.Context, uint8),
) failSafeRig {
	t.Helper()
	gc := newGencomm(t, core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor,
		FailSafeMaxSeconds: 900,
		Settings:           settings,
		Clock:              clk,
	})
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{
		SupportedFabrics:       5,
		FailSafe:               gc.FailSafeFabric,
		RearmFailSafeForFabric: gc.SetCurrentFabric,
	})
	if err != nil {
		t.Fatal(err)
	}
	gc.SetOnFailSafeArmed(func(context.Context, uint8) { oc.ClearPendingState() })
	if expired != nil {
		gc.SetOnFailSafeExpired(expired(oc))
	} else {
		gc.SetOnFailSafeExpired(oc.OnFailSafeExpiry)
	}
	gc.SetOnCommissioningComplete(func(context.Context, uint8) { oc.ClearPendingState() })
	return failSafeRig{gc: gc, oc: oc}
}

// armAndAddNOC arms the fail-safe over PASE and runs the commissioner's
// AddNOC sequence; it returns the new fabric's index.
func (r failSafeRig) armAndAddNOC(ctx context.Context, t *testing.T) uint8 {
	t.Helper()
	resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
	if err != nil {
		t.Fatalf("ArmFailSafe: %v", err)
	}
	if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe ErrorCode = %d", code)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	return idx
}

// commissioningComplete sends CommissioningComplete over a CASE session
// of fabric idx and reports whether it answered success.
func (r failSafeRig) commissioningComplete(ctx context.Context, t *testing.T, idx uint8) bool {
	t.Helper()
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x04, nil)
	if err != nil {
		return false
	}
	return resp.(core.CommissioningCompleteResponse).ErrorCode == core.CommissioningErrorOK
}

// TestGencomm_CommissioningCompleteCommitsThePendingNOCOrFails pins core2
// item 1a: CommissioningComplete does not answer success while the
// pending-NOC marker stands. When the marker's clear fails, it answers
// FAILURE (a store error, as the terms commit's) with the fail-safe still
// armed; a restart then rolls the fabric back, and a retried
// CommissioningComplete over a healthy store commits it — every outcome
// consistent with the answer the commissioner got.
func TestGencomm_CommissioningCompleteCommitsThePendingNOCOrFails(t *testing.T) {
	t.Parallel()
	for _, retry := range []bool{false, true} {
		name := "restart after the failure"
		if retry {
			name = "retry after the failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := &clearFailingStore{Store: memstore.New()}
			r := newFailSafeRig(t, st, st, nil, nil)
			idx := r.armAndAddNOC(ctx, t)

			st.fail.Store(true)
			ok := r.commissioningComplete(ctx, t, idx)
			if ok {
				restartOpcreds(t, st.Store)
				_, err := st.GetFabric(ctx, idx)
				t.Fatalf("CommissioningComplete answered success while the pending-NOC marker could not be cleared; the fabric after a restart: %v", err)
			}
			if !r.gc.FailSafeArmed() {
				t.Fatal("the failed CommissioningComplete disarmed the fail-safe")
			}
			if retry {
				st.fail.Store(false)
				if !r.commissioningComplete(ctx, t, idx) {
					t.Fatal("the retried CommissioningComplete over a healthy store failed")
				}
				restartOpcreds(t, st.Store)
				if _, err := st.GetFabric(ctx, idx); err != nil {
					t.Fatalf("a fabric CommissioningComplete committed did not survive the restart: %v", err)
				}
				return
			}
			restartOpcreds(t, st.Store)
			if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
				t.Fatalf("%d fabric(s) survived a restart after CommissioningComplete answered FAILURE", len(fabrics))
			}
		})
	}
}

// TestGencomm_ArmInTheExpiryGapWaitsForTheRollback pins core2 item 1b: the
// expiry watcher disarms and releases its lock before it runs the
// rollback hook. An arm in that gap — here the PASE auto-arm — used to be
// "fresh", so its hook (ClearPendingState) dropped the pending AddNOC
// before OnFailSafeExpiry read it, and the uncommitted fabric stayed. The
// expiry hook is held at the gap; synctest.Wait lets the arm run as far
// as it gets before the hook goes on.
func TestGencomm_ArmInTheExpiryGapWaitsForTheRollback(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
		st := memstore.New()
		atGap, release := make(chan struct{}), make(chan struct{})
		r := newFailSafeRig(t, st, st, clk, func(oc *core.OperationalCredentials) func(context.Context, uint8) {
			return func(ctx context.Context, fi uint8) {
				close(atGap)
				<-release
				oc.OnFailSafeExpiry(ctx, fi)
			}
		})
		r.armAndAddNOC(ctx, t)
		// Stop the new window's watcher however the test ends, so the bubble
		// has no goroutine left waiting on the fake clock.
		defer func() { _ = r.gc.ArmFailSafeFor(ctx, 0, 0) }()

		clk.Advance(61 * time.Second)
		<-atGap
		armed := make(chan struct{})
		go func() {
			defer close(armed)
			r.gc.AutoArmOnPaseEstablished(ctx)
		}()
		synctest.Wait()
		close(release)
		<-armed
		synctest.Wait()
		if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
			t.Fatalf("%d fabric(s) survived the expiry: the arm in the gap dropped the pending AddNOC before the rollback read it", len(fabrics))
		}
		if !r.gc.FailSafeArmed() {
			t.Fatal("the PASE auto-arm armed nothing")
		}
	})
}

// TestGencomm_ArmOverADueWindowRollsItBack is the other half of item 1b:
// a window whose deadline has passed but whose watcher has not run yet
// counted as fresh for ArmFailSafeFor, which dropped the pending AddNOC
// and replaced the watcher — the window's rollback never ran. The fake's
// wall step moves Now past the deadline without firing the timer.
func TestGencomm_ArmOverADueWindowRollsItBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	st := memstore.New()
	r := newFailSafeRig(t, st, st, clk, nil)
	r.armAndAddNOC(ctx, t)

	clk.StepWall(61 * time.Second)
	r.gc.AutoArmOnPaseEstablished(ctx)
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
		t.Fatalf("%d fabric(s) survived an arm over the due window: its rollback never ran", len(fabrics))
	}
	if !r.gc.FailSafeArmed() {
		t.Fatal("the PASE auto-arm armed nothing")
	}
	_ = r.gc.ArmFailSafeFor(ctx, 0, 0)
}
