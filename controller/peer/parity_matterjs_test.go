// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"testing"
	"time"
)

// TestParityTimingDefaults: every default of matter.js
// packages/protocol/src/peer/PeerTimingParameters.ts:184-209 at 0ceb202d.
func TestParityTimingDefaults(t *testing.T) {
	t.Parallel()
	d := DefaultTiming()
	rows := []struct {
		name string
		got  time.Duration
		want time.Duration
		line string
	}{
		{"defaultConnectionTimeout", d.DefaultConnectionTimeout, 90 * time.Second, ":188"},
		{"maxDelayBetweenInitialContactRetries", d.MaxDelayBetweenInitialContactRetries, 2 * time.Minute, ":184, :189"},
		{"delayBeforeNextAddress", d.DelayBeforeNextAddress, 45 * time.Second, ":193"},
		{"delayAfterNetworkError", d.DelayAfterNetworkError, 15 * time.Second, ":194"},
		{"delayAfterPeerError", d.DelayAfterPeerError, 5 * time.Minute, ":195"},
		{"delayAfterUnhandledError", d.DelayAfterUnhandledError, 2 * time.Minute, ":196"},
		{"kickThrottleInterval", d.KickThrottleInterval, 3 * time.Second, ":197"},
		{"kickMinRestartSaving", d.KickMinRestartSaving, time.Minute, ":199 (half of :184)"},
		{"kickRestartCooldown.addressChange", d.KickRestartCooldown.AddressChange, 30 * time.Minute, ":201"},
		{"kickRestartCooldown.connect", d.KickRestartCooldown.Connect, 10 * time.Minute, ":202"},
		{"addressChangeStabilizationDelay", d.AddressChangeStabilizationDelay, 10 * time.Second, ":204"},
		{"addressChangeProbeCooldown.minimum", d.AddressChangeProbeCooldown.Min, 10 * time.Minute, ":206"},
		{"addressChangeProbeCooldown.maximum", d.AddressChangeProbeCooldown.Max, 24 * time.Hour, ":207"},
	}
	for _, r := range rows {
		if r.got != r.want {
			t.Errorf("%s (PeerTimingParameters.ts%s) = %v, want %v", r.name, r.line, r.got, r.want)
		}
	}
	if d.KickMinRetransmissions != 2 {
		t.Errorf("kickMinRetransmissions (:198) = %d, want 2", d.KickMinRetransmissions)
	}
}

// TestParityTimingMerge: overrides apply field-wise, a group member leaves
// its sibling alone, an unset field keeps the base (PeerTimingParameters.ts:153-168).
func TestParityTimingMerge(t *testing.T) {
	t.Parallel()
	base := DefaultTiming()
	got := Merge(base, Timing{
		DelayBeforeNextAddress:     time.Second,
		KickMinRetransmissions:     4,
		KickRestartCooldown:        KickRestartCooldown{Connect: time.Minute},
		AddressChangeProbeCooldown: ProbeCooldown{Max: time.Hour},
	})
	want := base
	want.DelayBeforeNextAddress = time.Second
	want.KickMinRetransmissions = 4
	want.KickRestartCooldown.Connect = time.Minute
	want.AddressChangeProbeCooldown.Max = time.Hour
	if got != want {
		t.Fatalf("Merge = %+v, want %+v", got, want)
	}
	if Merge(base, Timing{}) != base {
		t.Fatal("an empty override changed the base")
	}
	all := Timing{
		DefaultConnectionTimeout: 1, MaxDelayBetweenInitialContactRetries: 2, DelayBeforeNextAddress: 3,
		DelayAfterNetworkError: 4, DelayAfterPeerError: 5, DelayAfterUnhandledError: 6, KickThrottleInterval: 7,
		KickMinRetransmissions: 8, KickMinRestartSaving: 9, KickRestartCooldown: KickRestartCooldown{10, 11},
		AddressChangeStabilizationDelay: 12, AddressChangeProbeCooldown: ProbeCooldown{13, 14},
	}
	if Merge(base, all) != all {
		t.Fatal("a full override did not replace every field")
	}
}
