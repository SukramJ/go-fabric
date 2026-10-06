// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestParityMatterJS_WindowCovering_TravellingMovement mirrors matter.js
// support/chip-testing TestWindowCoveringServer: with a MoveStep the lift
// travels in six steps, OperationalStatus reads Opening/Closing (global and
// lift) until the target is reached, and every step is reported
// (TC-WNCV-3.1 to 3.3).
func TestParityMatterJS_WindowCovering_TravellingMovement(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const step = 950 * time.Millisecond
		s := cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0b101, InitialPositionPercent100ths: 10000, MoveStep: step})
		var mu sync.Mutex
		var reported [][]uint32
		s.OnMatterAttributesChanged(func(ids []uint32) {
			mu.Lock()
			reported = append(reported, ids)
			mu.Unlock()
		})
		ctx := context.Background()
		if _, err := s.MatterInvoke(ctx, wire.WindowCoveringCmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		read := func(id uint32) any { v, _ := s.MatterRead(id); return v }
		if st := read(wire.WindowCoveringAttrOperationalStatus); st != uint8(0b0101) {
			t.Fatalf("OperationalStatus while opening = %v, want 5 (global and lift Opening)", st)
		}
		if tg := read(wire.WindowCoveringAttrTargetPositionLiftPercent100ths); tg != uint16(0) {
			t.Fatalf("target = %v, want 0", tg)
		}
		time.Sleep(step)
		synctest.Wait()
		if cur := read(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths); cur != uint16(10000-1666) {
			t.Fatalf("position after one step = %v, want 8334", cur)
		}
		time.Sleep(5 * step)
		synctest.Wait()
		if cur := read(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths); cur != uint16(0) {
			t.Fatalf("position after six steps = %v, want 0", cur)
		}
		if st := read(wire.WindowCoveringAttrOperationalStatus); st != uint8(0) {
			t.Fatalf("OperationalStatus after arrival = %v, want 0", st)
		}
		mu.Lock()
		n := len(reported)
		last := reported[n-1]
		mu.Unlock()
		if n != 6 || !slices.Contains(last, wire.WindowCoveringAttrOperationalStatus) {
			t.Fatalf("reported %d steps (last %v), want 6 ending with OperationalStatus", n, last)
		}

		// Closing, then a StopMotion mid-way: the lift stays put and the
		// target follows it.
		if _, err := s.MatterInvoke(ctx, wire.WindowCoveringCmdDownOrClose, nil); err != nil {
			t.Fatal(err)
		}
		if st := read(wire.WindowCoveringAttrOperationalStatus); st != uint8(0b1010) {
			t.Fatalf("OperationalStatus while closing = %v, want 10", st)
		}
		time.Sleep(2 * step)
		synctest.Wait()
		if _, err := s.MatterInvoke(ctx, wire.WindowCoveringCmdStopMotion, nil); err != nil {
			t.Fatal(err)
		}
		cur := read(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths)
		if cur != uint16(3332) || read(wire.WindowCoveringAttrTargetPositionLiftPercent100ths) != cur {
			t.Fatalf("after StopMotion: position %v target %v, want both 3332", cur, read(wire.WindowCoveringAttrTargetPositionLiftPercent100ths))
		}
		time.Sleep(10 * step)
		synctest.Wait()
		if read(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths) != cur {
			t.Fatal("the lift moved after StopMotion")
		}
	})
}
