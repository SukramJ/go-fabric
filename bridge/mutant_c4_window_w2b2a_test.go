// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/wire"
)

// armedFailSafeW2B2a reports a fail-safe armed.
type armedFailSafeW2B2a struct{}

func (armedFailSafeW2B2a) FailSafeArmed() bool { return true }

// TestBlockedOwnWindowConsultsNoFabricCounterW2B2a (C4,
// commissioning_window.go OpenOwnWindow, the early check): an own window
// opened while commissioning is blocked is refused with
// [ErrCommissioningBlocked] before the host's fabric counter is consulted.
func TestBlockedOwnWindowConsultsNoFabricCounterW2B2a(t *testing.T) {
	t.Parallel()
	w := NewCommissioningWindow()
	var counted atomic.Int32
	w.SetFabricCounter(func(context.Context) (int, error) {
		counted.Add(1)
		return 0, nil
	})
	w.SetBlocked(context.Background(), errors.New("attestation unavailable"))
	if err := w.OpenOwnWindow(context.Background(), 0); !errors.Is(err, ErrCommissioningBlocked) || w.IsOpen() {
		t.Fatalf("OpenOwnWindow = %v (open %v), want ErrCommissioningBlocked and closed", err, w.IsOpen())
	}
	if n := counted.Load(); n != 0 {
		t.Fatalf("the fabric counter was consulted %d times while blocked, want 0", n)
	}
}

// TestBlockedWindowWithArmedFailSafeIsNotBusyW2B2a (C4,
// commissioning_window.go OpenWindow, the early check): a blocked open
// answers [ErrCommissioningBlocked] — FAILURE — even while a fail-safe is
// armed (one armed over CASE outlives SetBlocked); Busy is only for a
// window that cannot open for a commissioning in progress.
func TestBlockedWindowWithArmedFailSafeIsNotBusyW2B2a(t *testing.T) {
	t.Parallel()
	w := NewCommissioningWindow()
	w.SetBlocked(context.Background(), errors.New("attestation unavailable"))
	w.SetFailSafeChecker(armedFailSafeW2B2a{})
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600})
	if !errors.Is(err, ErrCommissioningBlocked) || errors.Is(err, wire.ErrAdmCommBusy) || w.IsOpen() {
		t.Fatalf("OpenWindow = %v (open %v), want ErrCommissioningBlocked, not Busy, and closed", err, w.IsOpen())
	}
}

// TestWindowRefusalPrecedenceW2B2a (decision 688): every open of a
// window refused for more than one reason answers the first of stopped >
// blocked > busy — the order adminOpenRefusedLocked applies under the
// lock holds for the early checks too.
func TestWindowRefusalPrecedenceW2B2a(t *testing.T) {
	t.Parallel()
	blockedErr := errors.New("attestation unavailable")
	adminOpen := func(w *CommissioningWindow) error {
		return w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600})
	}
	ownOpen := func(w *CommissioningWindow) error { return w.OpenOwnWindow(context.Background(), 0) }
	for _, tc := range []struct {
		name    string
		stopped bool
		blocked bool
		armed   bool
		open    func(*CommissioningWindow) error
		want    error
	}{
		{name: "own/stopped and blocked", stopped: true, blocked: true, open: ownOpen, want: ErrStopped},
		{name: "admin/stopped and blocked", stopped: true, blocked: true, open: adminOpen, want: ErrStopped},
		{name: "admin/stopped and busy", stopped: true, armed: true, open: adminOpen, want: ErrStopped},
		{name: "admin/stopped, blocked and busy", stopped: true, blocked: true, armed: true, open: adminOpen, want: ErrStopped},
		{name: "admin/blocked and busy", blocked: true, armed: true, open: adminOpen, want: ErrCommissioningBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := NewCommissioningWindow()
			if tc.armed {
				w.SetFailSafeChecker(armedFailSafeW2B2a{})
			}
			if tc.blocked {
				w.SetBlocked(context.Background(), blockedErr)
			}
			if tc.stopped {
				w.stopTimer()
			}
			err := tc.open(w)
			if !errors.Is(err, tc.want) {
				t.Fatalf("open = %v, want %v", err, tc.want)
			}
			if errors.Is(tc.want, ErrStopped) && errors.Is(err, ErrCommissioningBlocked) {
				t.Fatalf("open = %v: a stopped window answers ErrStopped alone", err)
			}
			if w.IsOpen() {
				t.Fatal("the window opened")
			}
		})
	}
}
