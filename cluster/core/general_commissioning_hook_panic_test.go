// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestGencomm_HookPanicLeavesTheLockFree is R2-P2-rr-dev-03: a hook an arm
// or CommissioningComplete runs while it holds the cluster's lock released
// — the fresh start (onFailSafeStarting) or the rollback of a due window
// (onFailSafeExpired) — panics, and the dispatcher recovers it, as
// transport/udp's safeDispatch does. The cluster's lock is free afterwards
// on every path: before, the PASE auto-arm left it locked after a starting
// hook's panic, and every later read of the cluster blocked.
func TestGencomm_HookPanicLeavesTheLockFree(t *testing.T) {
	t.Parallel()
	const fabric = 1
	for _, c := range []struct {
		name string
		// due arms a window for fabric and moves the clock past it; the
		// expiry hook panics. Without it the starting hook panics.
		due bool
		run func(ctx context.Context, gc *core.GeneralCommissioning)
	}{
		{"starting hook, PASE auto-arm", false, func(ctx context.Context, gc *core.GeneralCommissioning) {
			gc.AutoArmOnPaseEstablished(ctx)
		}},
		{"starting hook, ArmFailSafe", false, func(ctx context.Context, gc *core.GeneralCommissioning) {
			_, _ = gc.MatterInvoke(im.WithFabricFilter(ctx, true, fabric), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		}},
		{"starting hook, ArmFailSafeFor", false, func(ctx context.Context, gc *core.GeneralCommissioning) {
			_ = gc.ArmFailSafeFor(ctx, 60, fabric)
		}},
		{"expiry hook, PASE auto-arm", true, func(ctx context.Context, gc *core.GeneralCommissioning) {
			gc.AutoArmOnPaseEstablished(ctx)
		}},
		{"expiry hook, ArmFailSafe", true, func(ctx context.Context, gc *core.GeneralCommissioning) {
			_, _ = gc.MatterInvoke(im.WithFabricFilter(ctx, true, fabric), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		}},
		{"expiry hook, CommissioningComplete", true, func(ctx context.Context, gc *core.GeneralCommissioning) {
			_, _ = gc.MatterInvoke(im.WithFabricFilter(ctx, true, fabric), 0x04, nil)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
			gc := newGencomm(t, core.GeneralCommissioningConfig{FailSafeMaxSeconds: 900, Clock: clk})
			defer func() { _ = gc.Close(ctx) }()
			if c.due {
				if err := gc.ArmFailSafeFor(ctx, 60, fabric); err != nil {
					t.Fatalf("ArmFailSafeFor: %v", err)
				}
				gc.SetOnFailSafeExpired(func(context.Context, uint8) { panic("expiry hook") })
				clk.StepWall(61 * time.Second)
			} else {
				gc.SetOnFailSafeStarting(func(context.Context) []uint8 { panic("starting hook") })
			}
			recovered := func() (p any) {
				defer func() { p = recover() }()
				c.run(ctx, gc)
				return nil
			}()
			if recovered == nil {
				t.Fatal("the hook did not panic: the path under test did not run it")
			}
			read := make(chan struct{})
			go func() {
				gc.FailSafeArmed()
				close(read)
			}()
			wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			select {
			case <-read:
			case <-wait.Done():
				t.Fatalf("FailSafeArmed still blocked after the recovered panic (%v): the cluster's lock stays held", recovered)
			}
		})
	}
}
