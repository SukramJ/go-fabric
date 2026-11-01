// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package switchserver

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestMatterQuiesceStopsTheTimers: MatterQuiesce, which the bridge calls
// when the server leaves the topology or stops, leaves none of the three
// timers armed and none of them runs afterwards — matter.js
// SwitchServer.ts:379-384 at 862ad75f ([Symbol.asyncDispose]; W3-club2-02).
func TestMatterQuiesceStopsTheTimers(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(0, 0))
	s, err := New(Config{Features: FeatureMS | FeatureMSR | FeatureMSL, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	s.mu.Lock()
	for _, slot := range []*timerSlot{&s.debounceTimer, &s.longPressTimer, &s.multiPressTimer} {
		s.startTimer(slot, time.Second, func() { ran++ })
	}
	s.mu.Unlock()
	if n := clk.Pending(); n != 3 {
		t.Fatalf("%d timers armed, want 3", n)
	}
	s.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce, want 0", n)
	}
	clk.Advance(time.Minute)
	if ran != 0 {
		t.Fatalf("%d timer runs after MatterQuiesce", ran)
	}
}
