// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
)

// TestZoneTickBlindDurationZero: a trigger whose BlindDuration is 0, which
// the schema allows, leaves the zone manager at its first blind tick — it
// is neither blind for 2^32 s nor keeps the 1-s timer running (review II,
// R2-P1-misc-04). A BlindDuration of 2 keeps it blind for one more tick.
func TestZoneTickBlindDurationZero(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		blind     uint32
		keptAfter []bool
	}{
		{0, []bool{false}},
		{1, []bool{false}},
		{2, []bool{true, false}},
	} {
		clk := clocktest.New(time.Unix(1_000_000, 0))
		cam := &demoCamera{clk: clk}
		cam.triggers = []*camTriggerContext{{
			trigger: zmdef.ZoneTriggerControlStruct{ZoneId: 1, BlindDuration: c.blind},
			blind:   true, remainingBlind: c.blind,
		}}
		for i, kept := range c.keptAfter {
			cam.zoneTick()
			cam.mu.Lock()
			n, armed := len(cam.triggers), cam.timer != nil
			var remaining uint32
			if n > 0 {
				remaining = cam.triggers[0].remainingBlind
			}
			cam.mu.Unlock()
			if (n == 1) != kept || armed != kept {
				t.Errorf("BlindDuration %d, blind tick %d: %d triggers (remaining blind %d s), timer armed %v; want kept %v",
					c.blind, i+1, n, remaining, armed, kept)
			}
		}
	}
}
