// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// A margin joins the base interval of every (re)transmission and is
// amplified with it (matter.js MRP.ts retransmissionIntervalOf,
// `baseInterval = … + additionalDelay`, at 862ad75f): the first
// retransmission waits (base + margin) × MARGIN × (1 + jitter), the later
// ones grow from the same sum.
func TestOutboundTrackerMarginJoinsTheBaseInterval(t *testing.T) {
	t.Parallel()
	const base = 300 * time.Millisecond
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	now := time.Unix(1_000_000, 0)
	for name, margin := range map[string]time.Duration{"without": 0, "with": 2 * time.Second} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tr := newOutboundReliableTracker(func(uint16, time.Time) time.Duration { return base }, nil)
			tr.TrackWithMargin(7, 1, 9, []byte{1}, dest, now, margin)
			p := tr.pending[outboundKey{1, 7}]
			lo := time.Duration(float64(base+margin) * mrp.MRPBackoffMargin)
			hi := time.Duration(float64(lo) * (1 + mrp.MRPBackoffJitterFactor))
			if w := p.nextSendAt.Sub(now); w < lo || w > hi {
				t.Fatalf("first wait %v, want %v to %v", w, lo, hi)
			}
			// The retransmission keeps the margin: the next wait grows
			// from the same sum.
			due := p.nextSendAt
			sent := 0
			tr.Tick(due, func(net.Addr, []byte) error { sent++; return nil })
			if sent != 1 {
				t.Fatalf("sent %d, want 1", sent)
			}
			if w := p.nextSendAt.Sub(due); w < lo || w > hi {
				t.Fatalf("second wait %v, want %v to %v", w, lo, hi)
			}
		})
	}
}
