// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestGencomm_FailSafeRolledBackHook pins the session owner's hook
// (SetOnFailSafeRolledBack, the bridge's): it runs wherever the fail-safe
// expires without a CommissioningComplete — the timer, ArmFailSafe with
// expiry 0, ExpireFailSafe — after the host's hook and without one, and not
// when the commissioning completes or nothing was armed. matter.js runs
// the PASE close inside the same rollback
// (packages/protocol/src/common/FailsafeContext.ts:271-279 at db021da0).
//
// Each case runs in a synctest bubble. The timer's hooks run on the
// watcher's goroutine, and what the case asserts — both hooks, in order,
// and no third — is true only once that goroutine has finished:
// synctest.Wait returns when it has, where a loop that left at the first
// recorded hook read the list between the two (review II, R2-P0-rv-B05).
func TestGencomm_FailSafeRolledBackHook(t *testing.T) {
	t.Parallel()
	type rig struct {
		gc  *core.GeneralCommissioning
		clk *clocktest.Fake
		mu  sync.Mutex
		ran []string
	}
	newRig := func(t *testing.T, withHost bool) *rig {
		t.Helper()
		r := &rig{clk: clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))}
		r.gc = newGencomm(t, core.GeneralCommissioningConfig{
			LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 900, Clock: r.clk,
		})
		note := func(what string) {
			r.mu.Lock()
			r.ran = append(r.ran, what)
			r.mu.Unlock()
		}
		if withHost {
			r.gc.SetOnFailSafeExpired(func(context.Context, uint8) { note("host") })
		}
		r.gc.SetOnFailSafeRolledBack(func(context.Context) { note("owner") })
		return r
	}
	hooks := func(r *rig) []string {
		r.mu.Lock()
		defer r.mu.Unlock()
		return slices.Clone(r.ran)
	}
	arm := func(t *testing.T, r *rig, seconds uint16) {
		t.Helper()
		resp, err := r.gc.MatterInvoke(context.Background(), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: seconds})
		if err != nil {
			t.Fatalf("ArmFailSafe(%d): %v", seconds, err)
		}
		if a := resp.(core.ArmFailSafeResponse); a.ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe(%d) ErrorCode = %d, want OK", seconds, a.ErrorCode)
		}
	}
	expire := map[string]func(t *testing.T, r *rig){
		"the timer": func(_ *testing.T, r *rig) {
			r.clk.Advance(61 * time.Second)
		},
		"ArmFailSafe with expiry 0": func(t *testing.T, r *rig) {
			t.Helper()
			arm(t, r, 0)
		},
		"ExpireFailSafe": func(_ *testing.T, r *rig) { r.gc.ExpireFailSafe(context.Background()) },
	}
	for name, run := range expire {
		for _, withHost := range []bool{true, false} {
			want := []string{"owner"}
			label := name + ", no host hook"
			if withHost {
				want = []string{"host", "owner"}
				label = name + ", after the host's hook"
			}
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					r := newRig(t, withHost)
					arm(t, r, 60)
					synctest.Wait()
					if got := hooks(r); len(got) != 0 {
						t.Fatalf("hooks ran on the arm: %v", got)
					}
					run(t, r)
					// Whatever the expiry started has run to its end.
					synctest.Wait()
					if got := hooks(r); !slices.Equal(got, want) {
						t.Fatalf("hooks ran %v, want %v", got, want)
					}
					if r.gc.FailSafeArmed() {
						t.Error("the fail-safe is still armed")
					}
				})
			})
		}
	}
	t.Run("not without an armed fail-safe", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			arm(t, r, 0)
			r.gc.ExpireFailSafe(context.Background())
			synctest.Wait()
			if got := hooks(r); len(got) != 0 {
				t.Errorf("hooks ran %v with nothing armed", got)
			}
		})
	})
	t.Run("detached", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			r.gc.SetOnFailSafeRolledBack(nil)
			arm(t, r, 60)
			r.gc.ExpireFailSafe(context.Background())
			synctest.Wait()
			if got := hooks(r); !slices.Equal(got, []string{"host"}) {
				t.Errorf("hooks ran %v, want the host's alone", got)
			}
		})
	})
}
