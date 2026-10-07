// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"testing"
	"time"
)

// TestGeneralDiagnosticsUpTimeCountsSuspendNeverLowered mirrors matter.js
// GeneralDiagnosticsServer.ts upTime after c0a7978d (#4614): the larger of
// the monotonic and the wall-clock elapsed time, so a host suspend (which
// stops the monotonic clock) still counts; never below a value already
// reported, so a backward wall-clock step cannot lower it; never negative.
func TestGeneralDiagnosticsUpTimeCountsSuspendNeverLowered(t *testing.T) {
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	steps := []struct {
		name       string
		mono, wall time.Duration
		want       time.Duration
	}{
		{"before any step, both clocks agree", 10 * time.Second, 10 * time.Second, 10 * time.Second},
		{"after an hour's suspend the wall clock is ahead", 20 * time.Second, time.Hour + 20*time.Second, time.Hour + 20*time.Second},
		{"an NTP step back does not lower it", 30 * time.Second, 25 * time.Second, time.Hour + 20*time.Second},
		{"it resumes once real time passes the mark", 2 * time.Hour, 2 * time.Hour, 2 * time.Hour},
	}
	for _, s := range steps {
		if got := g.upTimeFrom(s.mono, s.wall); got != s.want {
			t.Errorf("%s: upTime = %v, want %v", s.name, got, s.want)
		}
	}
	if got := NewGeneralDiagnostics(BootReasonPowerOnReboot).upTimeFrom(-time.Second, -time.Minute); got != 0 {
		t.Errorf("negative elapsed times: upTime = %v, want 0", got)
	}
	if v, ok := g.MatterRead(gendiagAttrUpTime); !ok || v.(uint64) < uint64((2*time.Hour).Seconds()) {
		t.Errorf("UpTime attribute %v went below the reported high-water mark", v)
	}
}
