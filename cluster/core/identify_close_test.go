// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// gatedIdentify is a host IdentifyHandler that does not return from the
// IdentifyTime it gates on until the test lets it — a handler that is slow,
// or waits for a lock the goroutine closing the server holds. It records an
// IdentifyTime when its handling of it returns.
type gatedIdentify struct {
	gate    uint16
	entered chan struct{} // closed when the gated call is in the handler
	release chan struct{} // closed by the test to let it return

	mu       sync.Mutex
	returned []uint16
}

func newGatedIdentify(gate uint16) *gatedIdentify {
	return &gatedIdentify{gate: gate, entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *gatedIdentify) MatterIdentify(_ context.Context, t uint16) error {
	if t == h.gate {
		close(h.entered)
		<-h.release
	}
	h.mu.Lock()
	h.returned = append(h.returned, t)
	h.mu.Unlock()
	return nil
}

func (h *gatedIdentify) MatterTriggerEffect(context.Context, uint8, uint8) error { return nil }

func (h *gatedIdentify) handled() []uint16 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.returned)
}

// await fails the test unless ch is closed before ctx ends.
func await(ctx context.Context, t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("%s: not within the test's bound", what)
	}
}

// armedIdentify returns a server on a fake clock whose countdown runs
// from seconds, with its ticker armed.
func armedIdentify(t *testing.T, h contract.IdentifyHandler, seconds uint16) (*core.Identify, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	id := core.NewIdentify()
	id.SetClock(clk)
	id.SetHandler(h)
	if _, err := id.MatterInvoke(context.Background(), 0x00, seconds); err != nil {
		t.Fatalf("MatterInvoke(Identify %d): %v", seconds, err)
	}
	for range 10_000 { // the countdown arms its ticker on its own goroutine
		if clk.Pending() > 0 {
			break
		}
		clk.Yield()
	}
	if clk.Pending() != 1 {
		t.Fatalf("%d timers armed, want the countdown's ticker", clk.Pending())
	}
	return id, clk
}

// TestIdentify_CloseDoesNotWaitForTheHostsHandler: Close ends the
// countdown and returns while the host's MatterIdentify is still running —
// for a request's handler the countdown's tick waits behind, and for the
// stop the countdown itself reports. A Close that waited there would wait
// for as long as the handler takes, and for ever when the handler needs a
// lock the closing goroutine holds (an endpoint removed under the host's
// device-model lock; review II, R2-P0-rv-B04).
func TestIdentify_CloseDoesNotWaitForTheHostsHandler(t *testing.T) {
	t.Parallel()

	t.Run("a request in the handler, the countdown's tick behind it", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h := newGatedIdentify(10)
		id, clk := armedIdentify(t, h, 5)

		requested := make(chan error, 1)
		go func() {
			_, err := id.MatterInvoke(context.Background(), 0x00, uint16(10))
			requested <- err
		}()
		await(ctx, t, h.entered, "the request reaching the host's handler")
		// A second passes inside the handler: the tick now waits for the
		// request. The yields let the countdown goroutine take the tick
		// before Close — without them a Close that waits could pass by
		// winning the race, never fail for it.
		clk.Advance(time.Second)
		for range 20 {
			clk.Yield()
		}

		closed := make(chan struct{})
		go func() {
			id.Close()
			close(closed)
		}()
		await(ctx, t, closed, "Close while a request's handler is in flight")
		if n := clk.Pending(); n != 0 {
			t.Errorf("%d timers armed after Close, want 0 — the countdown still runs", n)
		}

		close(h.release)
		select {
		case err := <-requested:
			if err != nil {
				t.Fatalf("the request: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("the request did not return once its handler did")
		}
		// The tick that waited gave up with the Close: nothing counted
		// the new time down, and the host was told nothing more.
		if v, _ := id.MatterRead(0x0000); v != uint16(10) {
			t.Errorf("IdentifyTime %v after the request, want 10", v)
		}
		if got := h.handled(); !slices.Equal(got, []uint16{5, 10}) {
			t.Errorf("the host handled %v, want [5 10]", got)
		}
	})

	t.Run("the countdown's own stop in the handler", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h := newGatedIdentify(0)
		id, clk := armedIdentify(t, h, 1)
		reported := make(chan struct{})
		var once sync.Once
		id.OnAttributeChange(func(contract.Change) { once.Do(func() { close(reported) }) })

		clk.Advance(time.Second)
		await(ctx, t, h.entered, "the countdown's stop reaching the host's handler")

		closed := make(chan struct{})
		go func() {
			id.Close()
			close(closed)
		}()
		await(ctx, t, closed, "Close while the countdown's stop is in the host's handler")
		if n := clk.Pending(); n != 0 {
			t.Errorf("%d timers armed after Close, want 0 — the countdown still runs", n)
		}
		// The zero was stored and reported without the host's answer: the
		// countdown's goroutine, which Close waited for, did both.
		select {
		case <-reported:
		default:
			t.Error("IdentifyTime reaching 0 was not reported while the host still handles the stop")
		}
		if v, _ := id.MatterRead(0x0000); v != uint16(0) {
			t.Errorf("IdentifyTime %v while the host handles the stop, want 0", v)
		}
		close(h.release)
	})
}

// TestIdentify_ARequestWaitsForTheCountdownsStop: the stop the countdown
// reports and a request that follows reach the host one after the other —
// the request's MatterIdentify does not start before the stop returned, so
// the host's last instruction is the request's, never a late "stop"
// (review II, R2-P0-conc-B07; kept when the stop moved off the countdown's
// goroutine, R2-P0-rv-B04).
func TestIdentify_ARequestWaitsForTheCountdownsStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h := newGatedIdentify(0)
	id, clk := armedIdentify(t, h, 1)
	t.Cleanup(id.Close)

	clk.Advance(time.Second)
	await(ctx, t, h.entered, "the countdown's stop reaching the host's handler")

	requested := make(chan error, 1)
	go func() {
		_, err := id.MatterInvoke(context.Background(), 0x00, uint16(7))
		requested <- err
	}()
	// Room for a request that did not wait to run its handler to the end;
	// one that waits is still waiting after any number of yields.
	for range 20 {
		clk.Yield()
	}
	if got := h.handled(); !slices.Equal(got, []uint16{1}) {
		t.Fatalf("the host handled %v while the stop was in flight, want [1]: the request overtook the stop", got)
	}
	close(h.release)
	select {
	case err := <-requested:
		if err != nil {
			t.Fatalf("the request: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the request did not return once the stop did")
	}
	if got := h.handled(); !slices.Equal(got, []uint16{1, 0, 7}) {
		t.Fatalf("the host handled %v, want [1 0 7]", got)
	}
	if v, _ := id.MatterRead(0x0000); v != uint16(7) {
		t.Errorf("IdentifyTime %v after the request, want 7", v)
	}
}
