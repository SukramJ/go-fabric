// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/MRPTest.ts at 862ad75f:
// the retransmission interval, its maximum and the peer response time per
// channel. The Go surface is RetransmissionInterval (rand01 = 1 is
// maxRetransmissionIntervalOf), MaxPeerResponseTime (the "udp" case) and
// ReliableMaxPeerResponseTime (the "tcp" and "ble" cases). Go has no
// network-profile margins (marginFor, additionalDelay, fixedBackoff), so
// those cases are not ported.

import (
	"math"
	"testing"
	"time"
)

// mrpParams is SessionParameters({ idleInterval: 10 s, activeInterval: 500 ms })
// (MRPTest.ts:32-35, :142-145).
var mrpParams = Intervals{Idle: 10 * time.Second, Active: 500 * time.Millisecond, ActiveThreshold: 4 * time.Second}

// mrpDefaults is SessionParameters() — idle 500 ms, active 300 ms, threshold
// 4000 ms (MRPTest.ts:250).
var mrpDefaults = Intervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 4 * time.Second}

// mrpAmplified is the backoff of MRPTest.ts:44-47 in milliseconds.
func mrpAmplified(base time.Duration, n int) float64 {
	return float64(base.Milliseconds()) * 1.1 * math.Pow(1.6, math.Max(0, float64(n-1)))
}

func TestMRPRetransmissionIntervalOf(t *testing.T) {
	t.Parallel()
	t.Run("adds no margin when additionalDelay is omitted", func(t *testing.T) {
		t.Parallel()
		// runtimeRangeFor(500 ms, 0, 0), MRPTest.ts:41-52.
		amp := mrpAmplified(mrpParams.Active, 0)
		lo := time.Duration(math.Floor(amp)) * time.Millisecond
		hi := time.Duration(math.Floor(amp*1.25)) * time.Millisecond
		for _, r := range []float64{0, 0.5, 0.999} {
			if got := RetransmissionInterval(0, mrpParams, true, r); got < lo || got > hi {
				t.Errorf("rand %v: interval = %v, want within [%v, %v]", r, got, lo, hi)
			}
		}
	})
}

func TestMRPMaxRetransmissionIntervalOf(t *testing.T) {
	t.Parallel()
	// maximumFor, MRPTest.ts:147-154.
	maximumFor := func(base time.Duration, n int) time.Duration {
		return time.Duration(math.Floor(mrpAmplified(base, n)*1.25)) * time.Millisecond
	}
	for _, tc := range []struct {
		name   string
		n      int
		active bool
		base   time.Duration
	}{
		{"uses the idle interval for retransmissions when the peer is not active", 1, false, mrpParams.Idle},
		{"uses the active interval for retransmissions when the peer is active", 1, true, mrpParams.Active},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, want := RetransmissionInterval(tc.n, mrpParams, tc.active, 1), maximumFor(tc.base, tc.n); got != want {
				t.Errorf("interval = %v, want %v", got, want)
			}
		})
	}
}

func TestMRPMaxPeerResponseTimeOf(t *testing.T) {
	t.Parallel()
	// The "ble" and "tcp" describes carry the same three cases and the same
	// formula (MRP.ts:90-92, :111-113).
	for _, channel := range []string{"BLE channel", "TCP channel"} {
		t.Run(channel, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name      string
				expected  time.Duration
				wantTotal time.Duration
			}{
				{"uses the 30s base when expectedProcessingTime is small", 2 * time.Second, 35 * time.Second},
				{"honors expectedProcessingTime when larger than the 30s base", 60 * time.Second, 65 * time.Second},
				{"falls back to the default expectedProcessingTime when omitted", DefaultExpectedProcessingTime, 35 * time.Second},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := ReliableMaxPeerResponseTime(tc.expected); got != tc.wantTotal {
						t.Errorf("timeout = %v, want %v", got, tc.wantTotal)
					}
				})
			}
		})
	}

	t.Run("UDP channel", func(t *testing.T) {
		t.Parallel()
		peer := mrpDefaults
		for _, tc := range []struct {
			name   string
			peer   *Intervals
			active bool
			want   time.Duration
		}{
			// MRPTest.ts:261, :273, :284.
			{"composes the active peer leg, the active return leg, processing time and buffer", &peer, true, 15458 * time.Millisecond},
			{"uses the idle peer leg when the peer is inactive while the return leg stays active", &peer, false, 18279 * time.Millisecond},
			{"drops the peer leg when peer session parameters are unknown", nil, true, 11229 * time.Millisecond},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := MaxPeerResponseTime(tc.peer, tc.active, mrpDefaults, DefaultExpectedProcessingTime); got != tc.want {
					t.Errorf("timeout = %v, want %v", got, tc.want)
				}
			})
		}
	})
}
