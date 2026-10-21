// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package switchserver

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestNewRefusesWhatTheModelRefuses: the definition's feature conformance,
// "min 2" of NumberOfPositions and MultiPressMax, and a neutral position or
// a start position outside the positions.
func TestNewRefusesWhatTheModelRefuses(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]Config{
		"LS and MS":             {Features: FeatureLS | FeatureMS},
		"neither LS nor MS":     {},
		"MSR with AS":           {Features: FeatureMS | FeatureMSR | FeatureAS | FeatureMSM},
		"one position":          {Features: FeatureMS, NumberOfPositions: 1},
		"MultiPressMax 1":       {Features: FeatureMS | FeatureMSR | FeatureMSM, MultiPressMax: 1},
		"start out of range":    {Features: FeatureMS, CurrentPosition: 2},
		"neutral out of range":  {Features: FeatureMS, MomentaryNeutralPosition: 2},
		"AS without MSM (M/AS)": {Features: FeatureMS | FeatureAS},
	} {
		if _, err := New(cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
}

// TestDefaultsAreMatterJSs: the State defaults (SwitchServer.ts:14-15,
// :465-475) and the attributes they serve.
func TestDefaultsAreMatterJSs(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Features: FeatureMS | FeatureMSR | FeatureMSL | FeatureMSM})
	if err != nil {
		t.Fatal(err)
	}
	if s.longPressDelay != 2*time.Second || s.multiPressDelay != 300*time.Millisecond || s.debounceDelay != 0 {
		t.Errorf("delays = %v / %v / %v, want 2s / 300ms / none", s.longPressDelay, s.multiPressDelay, s.debounceDelay)
	}
	for id, want := range map[uint32]uint8{swdef.AttrNumberOfPositions: 2, swdef.AttrCurrentPosition: 0, swdef.AttrMultiPressMax: 2} {
		if v, ok := s.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %d", id, v, ok, want)
		}
	}
	if fm, _ := s.MatterRead(spec.AttrFeatureMap); fm != uint32(0x1E) {
		t.Errorf("FeatureMap = %v, want 0x1E", fm)
	}
	s.SetLongPressDelay(0)
	s.SetMultiPressDelay(0)
	if s.longPressDelay != DefaultLongPressDelay || s.multiPressDelay != DefaultMultiPressDelay {
		t.Error("a zero delay is not the default")
	}
	s.SetEndpoint(7)
	if s.Endpoint() != 7 {
		t.Errorf("Endpoint = %d, want 7", s.Endpoint())
	}
}

// TestServerMatchesTheGeneratedDefinition holds the server's lists to the
// definition for each feature selection the daemon and the bridge build.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, f := range []Feature{
		FeatureLS,
		FeatureMS | FeatureMSR,
		FeatureMS | FeatureMSR | FeatureMSL,
		FeatureMS | FeatureMSR | FeatureMSL | FeatureMSM,
		FeatureMS | FeatureAS | FeatureMSL | FeatureMSM,
	} {
		s, err := New(Config{Features: f})
		if err != nil {
			t.Fatal(err)
		}
		spectest.CheckServer(t, s, swdef.Definition, uint32(f))
	}
}

// TestWireWriteIsRefused: every Switch attribute is "R V"; a controller's
// write of CurrentPosition answers UNSUPPORTED_WRITE and changes nothing
// (matter.js refuses a write to a read-only attribute before the state).
func TestWireWriteIsRefused(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Features: FeatureMS})
	if err != nil {
		t.Fatal(err)
	}
	err = s.MatterWrite(context.Background(), swdef.AttrCurrentPosition, uint64(1))
	var st interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &st) || st.MatterStatusCode() != im.StatusUnsupportedWrite {
		t.Fatalf("write = %v, want UNSUPPORTED_WRITE", err)
	}
	if s.CurrentPosition() != 0 {
		t.Fatal("a refused write moved CurrentPosition")
	}
}

// emitted records the events a server emits, by id.
type emitted struct {
	ids      []uint32
	payloads []any
	eps      []uint16
}

func (e *emitted) MatterEmitEvent(ep uint16, _, id uint32, data any, prio contract.EventPriority) {
	if prio != contract.EventPriorityInfo {
		panic("every Switch event is info")
	}
	e.ids = append(e.ids, id)
	e.payloads = append(e.payloads, data)
	e.eps = append(e.eps, ep)
}

// TestGesturesEmitAndMoveTheCurrentPosition: the gesture input emits the
// narrated event, gated by the feature selection, and moves CurrentPosition
// to the pressed position and back to the neutral one.
func TestGesturesEmitAndMoveTheCurrentPosition(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Features: FeatureMS | FeatureMSR, Endpoint: 4})
	if err != nil {
		t.Fatal(err)
	}
	var changed [][]uint32
	s.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; changed = append(changed, ids) })
	e := &emitted{}
	s.SetMatterEventEmitter(e)

	s.FireInitialPress(1)
	if s.CurrentPosition() != 1 {
		t.Fatalf("CurrentPosition after InitialPress = %d, want 1", s.CurrentPosition())
	}
	s.FireLongPress(1) // no MSL: dropped
	s.FireLongRelease(1)
	s.FireInitialPress(1)
	s.FireShortRelease(1)
	if s.CurrentPosition() != 0 {
		t.Fatalf("CurrentPosition after the release = %d, want 0", s.CurrentPosition())
	}
	want := []uint32{swdef.EventInitialPress, swdef.EventInitialPress, swdef.EventShortRelease}
	if !slices.Equal(e.ids, want) {
		t.Fatalf("events %v, want %v", e.ids, want)
	}
	if e.eps[0] != 4 || e.payloads[0] != (swdef.InitialPressEvent{NewPosition: 1}) {
		t.Errorf("first event %v on %d", e.payloads[0], e.eps[0])
	}
	if len(changed) != 4 {
		t.Errorf("%d CurrentPosition changes reported, want 4 (1, 0, 1, 0)", len(changed))
	}
	// A position input continues from the gestures' position.
	if err := s.SetCurrentPosition(1); err != nil {
		t.Fatal(err)
	}
	if e.ids[len(e.ids)-1] != swdef.EventInitialPress {
		t.Error("a press after the gestures reported no InitialPress")
	}
	// A gesture position outside the range emits but does not move.
	s.FireInitialPress(9)
	if s.CurrentPosition() != 1 {
		t.Error("an out-of-range gesture moved CurrentPosition")
	}
}

// TestTimersRunOnTheSystemClock: with no Clock the timers are the system's,
// and a stopped timer that already began does nothing.
func TestTimersRunOnTheSystemClock(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Features: FeatureMS | FeatureMSR | FeatureMSL, LongPressDelay: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan uint32, 4)
	s.SetMatterEventEmitter(emitFunc(func(id uint32) { got <- id }))
	if err := s.SetCurrentPosition(1); err != nil {
		t.Fatal(err)
	}
	for _, want := range []uint32{swdef.EventInitialPress, swdef.EventLongPress} {
		select {
		case id := <-got:
			if id != want {
				t.Fatalf("event 0x%02X, want 0x%02X", id, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no event 0x%02X", want)
		}
	}
}

type emitFunc func(id uint32)

func (f emitFunc) MatterEmitEvent(_ uint16, _, id uint32, _ any, _ contract.EventPriority) {
	f(id)
}

// TestStaleTimerRunDoesNothing: a timer run that began before its timer was
// stopped finds a newer generation and returns.
func TestStaleTimerRunDoesNothing(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(0, 0))
	s, err := New(Config{Features: FeatureMS | FeatureMSR | FeatureMSL, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	s.mu.Lock()
	s.startTimer(&s.longPressTimer, time.Second, func() { ran = true })
	s.longPressTimer.gen++ // stopped after its run began
	s.mu.Unlock()
	clk.Advance(time.Second)
	if ran {
		t.Fatal("a stale timer run went ahead")
	}
}
