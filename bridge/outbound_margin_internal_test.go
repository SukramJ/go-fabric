// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
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

// A failed send counts as the transmission and keeps the backoff the
// sweep scheduled: a socket that fails at every attempt spends the retry
// budget on the MRP schedule, not on consecutive pump ticks. matter.js
// counts the retransmission before it sends and, on a send error that is
// not SessionClosedError, still arms the next one with the computed
// backoff (MessageExchange.ts:905-906, :975-981 at 862ad75f).
func TestOutboundTrackerFailedSendFollowsTheBackoffSchedule(t *testing.T) {
	t.Parallel()
	const base = 300 * time.Millisecond
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	now := time.Unix(1_000_000, 0)
	tr := newOutboundReliableTracker(func(uint16, time.Time) time.Duration { return base }, nil)
	tr.Track(7, 1, 9, []byte{1}, dest, now)
	p := tr.pending[outboundKey{1, 7}]
	fail := func(net.Addr, []byte) error { return errors.New("network is unreachable") }

	tick := 50 * time.Millisecond // the pump's cadence
	first := p.nextSendAt
	if res := tr.Tick(first, fail); len(res) != 1 || res[0].Err == nil {
		t.Fatalf("first retransmission: %+v, want one send error", res)
	}
	if p.retries != 1 {
		t.Fatalf("retries = %d, want 1 (the failed send counts)", p.retries)
	}
	lo := time.Duration(float64(base) * mrp.MRPBackoffMargin)
	if w := p.nextSendAt.Sub(first); w < lo {
		t.Fatalf("next attempt %v after the failure, want the backoff (at least %v)", w, lo)
	}
	// Six pump ticks (300 ms) stay inside the backoff: nothing is sent.
	for i := 1; i <= 6; i++ {
		if res := tr.Tick(first.Add(time.Duration(i)*tick), fail); len(res) != 0 {
			t.Fatalf("tick %d after the failure sent again: %+v", i, res)
		}
	}

	// The whole budget is spent on the schedule: the entry is abandoned
	// only after MaxRetransmissions attempts, each at its backoff.
	abandoned := false
	for i := 0; i < 100 && !abandoned; i++ {
		for _, r := range tr.Tick(p.nextSendAt, fail) {
			if errors.Is(r.Err, mrp.ErrMaxRetransmissionsReached) {
				abandoned = true
			}
		}
	}
	if !abandoned {
		t.Fatal("entry never abandoned")
	}
}
