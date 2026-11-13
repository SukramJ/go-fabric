// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
)

// returns runs f and fails the test when it has not returned within a
// second: a change listener that sets the server again must not block
// the call that notified it.
func returns(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	wait, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case <-done:
	case <-wait.Done():
		t.Fatalf("%s: a change listener that sets the server again never returned (deadlock)", what)
	}
}

// onceListener registers a change listener that runs set once, on the
// first change of attrID.
func onceListener(o *sensing.Occupancy, attrID uint32, set func()) {
	fired := false
	o.OnAttributeChange(func(c contract.Change) {
		if fired || !slices.Contains(c.Attributes, attrID) {
			return
		}
		fired = true
		set()
	})
}

// TestOccupancyListenerSetsTheServerAgain (review II, R2-P1-dr-rest-01):
// a host listener that sets the same server from its change notification
// returns, and its set is the last word — for a host report, a firing
// hold timer and a controller's HoldTime write. With a lock held across
// the store, each of the three deadlocked.
func TestOccupancyListenerSetsTheServerAgain(t *testing.T) {
	t.Parallel()
	cfg := func(c sensing.OccupancyConfig) sensing.OccupancyConfig {
		c.Features = occdef.FeaturePassiveInfrared | occdef.FeatureOccupancyEvent
		c.Attributes = holdTimeAttrs
		c.HoldTimeLimits = occLimits(1, 10, 1)
		c.HoldTime = ptr[uint16](5)
		return c
	}
	t.Run("host report", func(t *testing.T) {
		t.Parallel()
		o, em := newOcc(t, cfg(sensing.OccupancyConfig{Clock: newFake()}))
		onceListener(o, occdef.AttrOccupancy, func() {
			_ = o.SetAttributes(map[uint32]any{occdef.AttrOccupancy: occdef.OccupancyBitmap(0)})
		})
		occ := occdef.OccupancyOccupied
		var err error
		returns(t, "host report", func() { err = occdef.Wrap(o).Set(occdef.Patch{Occupancy: &occ}) })
		if err != nil {
			t.Fatal(err)
		}
		// Unoccupied with the hold time served starts the hold timer.
		if !occupied(t, o) {
			t.Fatalf("unoccupied before the hold timer ran out")
		}
		if got := occupancyEvents(em); !slices.Equal(got, []occdef.OccupancyBitmap{occdef.OccupancyOccupied}) {
			t.Fatalf("events %v, want [occupied]", got)
		}
	})
	t.Run("hold timer", func(t *testing.T) {
		t.Parallel()
		clk := newFake()
		o, em := newOcc(t, cfg(sensing.OccupancyConfig{Clock: clk}))
		setOccupied(t, o, true)
		setOccupied(t, o, false) // arms the 5-s hold timer
		onceListener(o, occdef.AttrOccupancy, func() {
			_ = o.SetAttributes(map[uint32]any{occdef.AttrOccupancy: occdef.OccupancyOccupied})
		})
		returns(t, "hold timer", func() { clk.Advance(5 * time.Second) })
		if !occupied(t, o) {
			t.Fatalf("unoccupied: the listener's occupied report, made after the timer's store, was not the last word")
		}
		want := []occdef.OccupancyBitmap{occdef.OccupancyOccupied, 0, occdef.OccupancyOccupied}
		if got := occupancyEvents(em); !slices.Equal(got, want) {
			t.Fatalf("events %v, want %v (in the order the changes were made)", got, want)
		}
	})
	t.Run("controller HoldTime write", func(t *testing.T) {
		t.Parallel()
		o, _ := newOcc(t, cfg(sensing.OccupancyConfig{Clock: newFake()}))
		onceListener(o, occdef.AttrHoldTime, func() {
			_ = o.SetAttributes(map[uint32]any{occdef.AttrHoldTime: uint16(8)})
		})
		var err error
		returns(t, "HoldTime write", func() { err = writeU16(o, occdef.AttrHoldTime, 7) })
		if err != nil {
			t.Fatal(err)
		}
		if attr := readU16(t, o, occdef.AttrHoldTime); attr != 8 || o.HoldTime() != 8 {
			t.Fatalf("HoldTime attribute %d, hold time %d; want both 8, the listener's later set", attr, o.HoldTime())
		}
	})
}
