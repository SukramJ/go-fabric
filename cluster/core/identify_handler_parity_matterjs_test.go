// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// recordingIdentify is a host IdentifyHandler that records every
// IdentifyTime it is handed and refuses when refuse is set.
type recordingIdentify struct {
	mu     sync.Mutex
	times  []uint16
	refuse error
}

func (r *recordingIdentify) MatterIdentify(_ context.Context, t uint16) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuse != nil {
		return r.refuse
	}
	r.times = append(r.times, t)
	return nil
}

func (r *recordingIdentify) MatterTriggerEffect(context.Context, uint8, uint8) error { return nil }

func (r *recordingIdentify) seen() []uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint16(nil), r.times...)
}

// TestParityMatterJS_IdentifyTimeReachesTheHost: an IdentifyTime write
// reaches the host's handler as the Identify command does, and the
// countdown reaching zero tells it to stop — matter.js reacts to
// identifyTime$Changed from the attribute and the command alike and its
// tick drives the same reactor to stopIdentifying
// (packages/node/src/behaviors/identify/IdentifyServer.ts:57-58,
// :100-127 at 862ad75f; W3-clucore-02).
func TestParityMatterJS_IdentifyTimeReachesTheHost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, entry := range []struct {
		name string
		run  func(*core.Identify) error
	}{
		{"write", func(id *core.Identify) error { return id.MatterWrite(ctx, 0x0000, uint64(2)) }},
		{"command", func(id *core.Identify) error { _, err := id.MatterInvoke(ctx, 0x00, uint16(2)); return err }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
			id := core.NewIdentify()
			id.SetClock(clk)
			t.Cleanup(id.Close)
			h := &recordingIdentify{}
			id.SetHandler(h)
			if err := entry.run(id); err != nil {
				t.Fatal(err)
			}
			for tick := range 2 {
				for range 10_000 {
					if clk.Pending() > 0 {
						break
					}
					clk.Yield()
				}
				clk.Advance(time.Second)
				want := uint16(1 - tick)
				for range 10_000 {
					if v, _ := id.MatterRead(0x0000); v == want {
						break
					}
					clk.Yield()
				}
			}
			for range 10_000 {
				if len(h.seen()) == 2 {
					break
				}
				clk.Yield()
			}
			if got := h.seen(); len(got) != 2 || got[0] != 2 || got[1] != 0 {
				t.Fatalf("handler saw %v, want [2 0]: the %s and the stop at zero", got, entry.name)
			}
		})
	}

	t.Run("a refusing handler refuses the write", func(t *testing.T) {
		t.Parallel()
		id := core.NewIdentify()
		t.Cleanup(id.Close)
		id.SetHandler(&recordingIdentify{refuse: contract.StatusError{Status: uint8(im.StatusBusy)}})
		err := id.MatterWrite(ctx, 0x0000, uint64(5))
		if se, ok := errors.AsType[contract.StatusError](err); !ok || se.Status != uint8(im.StatusBusy) {
			t.Fatalf("write: %v, want the handler's BUSY", err)
		}
		if v, _ := id.MatterRead(0x0000); v != uint16(0) {
			t.Fatalf("IdentifyTime %v after a refused write, want 0", v)
		}
	})
}

// tickingIdentify is a host IdentifyHandler whose handling of one
// IdentifyTime takes as long as the countdown's next tick: while it handles
// during, it advances the clock by a second and gives the countdown time to
// act on the tick before it returns.
type tickingIdentify struct {
	recordingIdentify
	clk     *clocktest.Fake
	during  uint16
	stopped chan struct{}
}

func (h *tickingIdentify) MatterIdentify(ctx context.Context, t uint16) error {
	if err := h.recordingIdentify.MatterIdentify(ctx, t); err != nil {
		return err
	}
	switch t {
	case h.during:
		h.clk.Advance(time.Second)
		select {
		case <-h.stopped:
		case <-time.After(300 * time.Millisecond):
		}
	case 0:
		close(h.stopped)
	}
	return nil
}

// TestIdentifyCountdownStopDoesNotOvertakeAWrite: the countdown's last tick
// and a request that sets a new IdentifyTime are one after the other, never
// interleaved — matter.js's tick is a callback under the behaviour's lock
// and its handler a reactor of the value the tick and a request both set
// (IdentifyServer.ts:54, :58, :108-114, :121-127 at db021da0), so the
// host is told to stop either before the new time arrives or not at all.
// Here the tick fires while the host handles the write of 10: the host's
// last instruction must not be "stop" while IdentifyTime counts from 10
// (review II, R2-P0-conc-B07).
func TestIdentifyCountdownStopDoesNotOvertakeAWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, entry := range []struct {
		name string
		run  func(*core.Identify) error
	}{
		{"write", func(id *core.Identify) error { return id.MatterWrite(ctx, 0x0000, uint64(10)) }},
		{"command", func(id *core.Identify) error { _, err := id.MatterInvoke(ctx, 0x00, uint16(10)); return err }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
			id := core.NewIdentify()
			id.SetClock(clk)
			t.Cleanup(id.Close)
			h := &tickingIdentify{clk: clk, during: 10, stopped: make(chan struct{})}
			id.SetHandler(h)
			// One second left on the countdown.
			if _, err := id.MatterInvoke(ctx, 0x00, uint16(1)); err != nil {
				t.Fatal(err)
			}
			for range 10_000 {
				if clk.Pending() > 0 {
					break
				}
				clk.Yield()
			}
			// The request of 10 s; the tick fires inside the handler.
			if err := entry.run(id); err != nil {
				t.Fatal(err)
			}
			// The tick that fired is the new countdown's first: 10 → 9.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if v, _ := id.MatterRead(0x0000); v == uint16(9) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			v, _ := id.MatterRead(0x0000)
			if got := h.seen(); len(got) != 2 || got[0] != 1 || got[1] != 10 || v != uint16(9) {
				t.Fatalf("the host saw %v and IdentifyTime is %v; want [1 10] and 9: the countdown's stop overtook the %s", got, v, entry.name)
			}
		})
	}
}
