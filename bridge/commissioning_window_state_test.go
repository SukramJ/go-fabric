// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/wire"
)

// blockingFailSafe stands in for a SetBlocked on another goroutine after
// OpenWindow passed its block check: it blocks the window while OpenWindow
// asks whether a fail-safe is armed.
type blockingFailSafe struct{ w *CommissioningWindow }

func (p blockingFailSafe) FailSafeArmed() bool {
	p.w.SetBlocked(context.Background(), errors.New("attestation unavailable"))
	return false
}

// TestWindowDoesNotOpenWhileBlocked: a block set between an open's first
// check and the moment the window opens refuses the open — the block is
// read again where the window opens, under its lock — for the node's own
// window and an administrator's (review II, R2-P6-fsm-0305).
func TestWindowDoesNotOpenWhileBlocked(t *testing.T) {
	t.Parallel()
	t.Run("own window", func(t *testing.T) {
		t.Parallel()
		w := NewCommissioningWindow()
		w.SetFabricCounter(func(ctx context.Context) (int, error) {
			w.SetBlocked(ctx, errors.New("attestation unavailable"))
			return 0, nil
		})
		if err := w.OpenOwnWindow(context.Background(), 0); !errors.Is(err, ErrCommissioningBlocked) || w.IsOpen() {
			t.Fatalf("OpenOwnWindow: err=%v open=%v, want ErrCommissioningBlocked and closed", err, w.IsOpen())
		}
	})
	t.Run("administrator's window", func(t *testing.T) {
		t.Parallel()
		w := NewCommissioningWindow()
		w.SetFailSafeChecker(blockingFailSafe{w})
		if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); !errors.Is(err, ErrCommissioningBlocked) || w.IsOpen() {
			t.Fatalf("OpenWindow: err=%v open=%v, want ErrCommissioningBlocked and closed", err, w.IsOpen())
		}
	})
}

// TestDiscriminatorIsZeroUnlessAnAdministratorsWindowIsOpen: Discriminator
// names the open administrator's window's discriminator and is 0 otherwise
// — after that window closed, and during the node's own window (review II,
// R2-P6-api-0309).
func TestDiscriminatorIsZeroUnlessAnAdministratorsWindowIsOpen(t *testing.T) {
	t.Parallel()
	w := NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600, Discriminator: 0x0ABC}); err != nil {
		t.Fatal(err)
	}
	if got := w.Discriminator(); got != 0x0ABC {
		t.Fatalf("Discriminator of the open window = %#x", got)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.Discriminator(); got != 0 {
		t.Fatalf("Discriminator after the window closed = %#x, want 0", got)
	}
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := w.Discriminator(); got != 0 {
		t.Fatalf("Discriminator during the node's own window = %#x, want 0", got)
	}
}

// TestStoppedWindowReadsClosed: a window open when the bridge stops reads
// closed afterwards; the stop runs no transition hook — the bridge is
// stopping, there is nothing to announce (review II, N-life-3).
func TestStoppedWindowReadsClosed(t *testing.T) {
	t.Parallel()
	w := NewCommissioningWindow()
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	hooks := 0
	w.SetTransitionHook(func() { hooks++ })
	w.stopTimer()
	if w.IsOpen() || w.IsOwnWindow() {
		t.Fatalf("after the stop: open=%v own=%v, want closed", w.IsOpen(), w.IsOwnWindow())
	}
	if hooks != 0 {
		t.Fatalf("the stop ran %d transition hooks, want none", hooks)
	}
}
