// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/cover"
	wcdef "thingfabric.dev/go-fabric/cluster/spec/windowcovering"
	"thingfabric.dev/go-fabric/contract"
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
		s.OnAttributeChange(func(change contract.Change) {
			ids := change.Attributes
			mu.Lock()
			reported = append(reported, ids)
			mu.Unlock()
		})
		ctx := context.Background()
		if _, err := s.MatterInvoke(ctx, wcdef.CmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		read := func(id uint32) any { v, _ := s.MatterRead(id); return v }
		if st := read(wcdef.AttrOperationalStatus); st != uint8(0b0101) {
			t.Fatalf("OperationalStatus while opening = %v, want 5 (global and lift Opening)", st)
		}
		if tg := read(wcdef.AttrTargetPositionLiftPercent100ths); tg != uint16(0) {
			t.Fatalf("target = %v, want 0", tg)
		}
		time.Sleep(step)
		synctest.Wait()
		if cur := read(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(10000-1666) {
			t.Fatalf("position after one step = %v, want 8334", cur)
		}
		time.Sleep(5 * step)
		synctest.Wait()
		if cur := read(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(0) {
			t.Fatalf("position after six steps = %v, want 0", cur)
		}
		if st := read(wcdef.AttrOperationalStatus); st != uint8(0) {
			t.Fatalf("OperationalStatus after arrival = %v, want 0", st)
		}
		mu.Lock()
		n := len(reported)
		last := reported[n-1]
		mu.Unlock()
		if n != 6 || !slices.Contains(last, wcdef.AttrOperationalStatus) {
			t.Fatalf("reported %d steps (last %v), want 6 ending with OperationalStatus", n, last)
		}

		// Closing, then a StopMotion mid-way: the lift stays put and the
		// target follows it.
		if _, err := s.MatterInvoke(ctx, wcdef.CmdDownOrClose, nil); err != nil {
			t.Fatal(err)
		}
		if st := read(wcdef.AttrOperationalStatus); st != uint8(0b1010) {
			t.Fatalf("OperationalStatus while closing = %v, want 10", st)
		}
		time.Sleep(2 * step)
		synctest.Wait()
		if _, err := s.MatterInvoke(ctx, wcdef.CmdStopMotion, nil); err != nil {
			t.Fatal(err)
		}
		cur := read(wcdef.AttrCurrentPositionLiftPercent100ths)
		if cur != uint16(3332) || read(wcdef.AttrTargetPositionLiftPercent100ths) != cur {
			t.Fatalf("after StopMotion: position %v target %v, want both 3332", cur, read(wcdef.AttrTargetPositionLiftPercent100ths))
		}
		time.Sleep(10 * step)
		synctest.Wait()
		if read(wcdef.AttrCurrentPositionLiftPercent100ths) != cur {
			t.Fatal("the lift moved after StopMotion")
		}
	})
}

// TestTravelFromAPositionTheHostMadeUnknown: the host sets the lift's
// position unknown during a travel. The remaining steps move nothing and
// report nothing — the position stays null — and the travel still ends
// after its sixth step: a position the host reports afterwards stays put
// (the tick's unknown-position branch, windowcovering_server.go tick).
func TestTravelFromAPositionTheHostMadeUnknown(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const step = 950 * time.Millisecond
		srv := cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0b101, InitialPositionPercent100ths: 10000, MoveStep: step})
		typed := wcdef.Wrap(srv)
		ctx := context.Background()
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		read := func(id uint32) any { v, _ := srv.MatterRead(id); return v }
		time.Sleep(step)
		synctest.Wait()
		if cur := read(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(8334) {
			t.Fatalf("position after one step = %v, want 8334", cur)
		}
		if err := typed.Set(wcdef.Patch{Null: []uint32{wcdef.AttrCurrentPositionLiftPercent100ths}}); err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var reported [][]uint32
		srv.OnAttributeChange(func(change contract.Change) {
			mu.Lock()
			reported = append(reported, change.Attributes)
			mu.Unlock()
		})
		time.Sleep(2 * step)
		synctest.Wait()
		if cur := read(wcdef.AttrCurrentPositionLiftPercent100ths); cur != nil {
			t.Errorf("position two steps after the host made it unknown = %v, want null", cur)
		}
		time.Sleep(3 * step) // the sixth step ends the travel
		synctest.Wait()
		mu.Lock()
		steps := len(reported)
		mu.Unlock()
		if steps != 0 {
			t.Errorf("the steps over an unknown position reported %v, want nothing", reported)
		}
		srv.SetCurrentPosition(new(uint16(5000)), nil)
		time.Sleep(10 * step)
		synctest.Wait()
		if cur := read(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(5000) {
			t.Errorf("the host's position after the travel ended = %v, want 5000 kept", cur)
		}
	})
}
