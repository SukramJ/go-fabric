// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/cover"
	"thingfabric.dev/go-fabric/cluster/spec"
	wcdef "thingfabric.dev/go-fabric/cluster/spec/windowcovering"
	"thingfabric.dev/go-fabric/contract"
)

// TestTravelStaysInsideTheRangeAfterAHostPosition (R2-P6-fsm-1502): a
// host position reported during a simulated travel moves the base the
// remaining steps add to; no step passes the target or leaves 0..10000.
func TestTravelStaysInsideTheRangeAfterAHostPosition(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const step = time.Second
		s := cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0b101, InitialPositionPercent100ths: 10000, MoveStep: step})
		if _, err := s.MatterInvoke(context.Background(), wcdef.CmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(step)
		synctest.Wait()
		low := uint16(100)
		s.SetCurrentPosition(&low, nil)
		for range 6 {
			time.Sleep(step)
			synctest.Wait()
			cur, _ := s.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths)
			if n, _ := cur.(uint16); n > 100 {
				t.Fatalf("CurrentPositionLiftPercent100ths %d: a step went past the target 0 from the host's 100", n)
			}
		}
		if cur, _ := s.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(0) {
			t.Errorf("the travel ended at %v, want the target 0", cur)
		}
	})
}

// TestMatterQuiesceEndsATravel (R2-P6-conc-1507): MatterQuiesce stops a
// travel in progress where it is; no step runs and nothing is reported
// after it, and a later command moves the covering again.
func TestMatterQuiesceEndsATravel(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const step = time.Second
		s := cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0b101, InitialPositionPercent100ths: 10000, MoveStep: step})
		q, ok := any(s).(contract.ClusterQuiescer)
		if !ok {
			t.Fatal("WindowCoveringServer has no MatterQuiesce: its step timer outlives the bridge's use of it")
		}
		var reports atomic.Int32
		s.OnAttributeChange(func(contract.Change) { reports.Add(1) })
		if _, err := s.MatterInvoke(context.Background(), wcdef.CmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(step)
		synctest.Wait()
		before, _ := s.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths)
		q.MatterQuiesce()
		n := reports.Load()
		time.Sleep(10 * step)
		synctest.Wait()
		if got := reports.Load(); got != n {
			t.Errorf("%d reports after MatterQuiesce, want none", got-n)
		}
		if after, _ := s.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths); after != before {
			t.Errorf("the position moved from %v to %v after MatterQuiesce", before, after)
		}
		if _, err := s.MatterInvoke(context.Background(), wcdef.CmdDownOrClose, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(6 * step)
		synctest.Wait()
		if cur, _ := s.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths); cur != uint16(10000) {
			t.Errorf("a command after MatterQuiesce left the position at %v, want 10000", cur)
		}
	})
}

// TestNewRefusesAnInitialPositionAboveTheRange (R2-P6-err-1562): New
// holds the initial positions of a position-aware axis to the attribute's
// "max 10000", as the host's reports are held.
func TestNewRefusesAnInitialPositionAboveTheRange(t *testing.T) {
	t.Parallel()
	for _, cfg := range []cover.Config{
		{FeatureMap: 0b101, InitialPositionPercent100ths: 12000},
		{Type: 7, EndProductType: 9, FeatureMap: 0b10010, InitialTiltPositionPercent100ths: 10001}, // tilt only
	} {
		if _, err := cover.New(cfg); !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("New(%+v) = %v, want spec.ErrInvalidValue", cfg, err)
		}
	}
	// Without the position-aware feature, or with an unknown position, the
	// value is not served and not judged.
	for _, cfg := range []cover.Config{
		{FeatureMap: 0b1, InitialPositionPercent100ths: 12000},
		{FeatureMap: 0b101, InitialPositionPercent100ths: 12000, InitialPositionUnknown: true},
		{FeatureMap: 0b101, InitialPositionPercent100ths: 10000},
	} {
		if _, err := cover.New(cfg); err != nil {
			t.Errorf("New(%+v) = %v, want a server", cfg, err)
		}
	}
}
