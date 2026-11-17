// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestChunkStatusResponseTimeoutSumsBothDirections (review II,
// R2-P1-req2-12): the bound on a chunk's StatusResponse is matter.js's
// maximum peer response time for a UDP session (MRP.ts:100-108 at
// db021da0): the peer's retransmission schedule, plus this node's own —
// its intervals, active at first and idle once the wait passes its active
// threshold (:198-224) — plus the expected processing time and the
// buffer. The peer's interval is the default idle 500 ms here (no session
// resolver); each schedule is five sends at margin 1.1 and full jitter
// 1.25, growing by 1.6 from the second retransmission.
func TestChunkStatusResponseTimeoutSumsBothDirections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		local SessionIntervals
		want  time.Duration
	}{
		// peer 687.5 ms × (1+1+1.6+2.56+4.096) = 7051.0 ms; local active
		// 300 ms throughout = 4230.6 ms; + 2 s + 5 s.
		{"local active throughout", SessionIntervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 4 * time.Second}, 18281600 * time.Microsecond},
		// local: 412.5 + 412.5 + 660 active, then past 1 s idle: 1760 + 2816
		// = 6061 ms.
		{"local idle past its threshold", SessionIntervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: time.Second}, 20112 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", SessionIntervals: tc.local}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := b.chunkStatusResponseTimeout(nil, 0)
			if diff := got - tc.want; diff < -time.Millisecond || diff > time.Millisecond {
				t.Fatalf("chunk StatusResponse bound = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestChunkStatusResponseTimeoutOverAConnection: over a reliable connection
// (TCP, BLE) the bound is max(30 s, 2 s) + 5 s = 35 s whatever the session
// intervals, as matter.js's maxPeerResponseTimeOf for such a channel
// (packages/protocol/src/protocol/MRP.ts:90-92, :111-113 at db021da0); a UDP
// peer keeps the retransmission sum.
func TestChunkStatusResponseTimeoutOverAConnection(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var c transport.Conn = &sizedConn{size: 64_000}
	if got := b.chunkStatusResponseTimeout(b.routeFor(c), 1); got != 35*time.Second {
		t.Fatalf("bound over a connection = %v, want 35s", got)
	}
	if got := b.chunkStatusResponseTimeout(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, 1); got == 35*time.Second {
		t.Fatal("a UDP peer took the connection's bound")
	}
}

// TestChunkStatusResponseTimeoutDefaultsZeroIntervals: a zero local idle,
// active interval or active threshold is unset and takes its default, so
// the bound equals the one with the defaults stated (R2-P6-bound-0813).
func TestChunkStatusResponseTimeoutDefaultsZeroIntervals(t *testing.T) {
	t.Parallel()
	bound := func(si SessionIntervals) time.Duration {
		b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", SessionIntervals: si}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return b.chunkStatusResponseTimeout(nil, 0)
	}
	defaults := SessionIntervals{Idle: mrp.SessionIdleIntervalDefault, Active: mrp.SessionActiveIntervalDefault, ActiveThreshold: mrp.SessionActiveThresholdDefault}
	// A threshold short enough that the schedule reaches the idle interval.
	short := defaults
	short.ActiveThreshold = time.Second
	for _, c := range []struct {
		name       string
		si, stated SessionIntervals
	}{
		{"idle 0", SessionIntervals{Active: short.Active, ActiveThreshold: short.ActiveThreshold}, short},
		{"active 0", SessionIntervals{Idle: short.Idle, ActiveThreshold: short.ActiveThreshold}, short},
		{"threshold 0", SessionIntervals{Idle: defaults.Idle, Active: defaults.Active}, defaults},
	} {
		if got, want := bound(c.si), bound(c.stated); got != want {
			t.Errorf("%s: bound %v, want %v (the defaults')", c.name, got, want)
		}
	}
}

// TestMaxScheduleTimeLeavesActiveOnlyPastTheThreshold: the schedule stays
// on the active interval while the wait has reached the threshold exactly
// and goes idle only once it passes it (R2-P6-bound-0813).
func TestMaxScheduleTimeLeavesActiveOnlyPastTheThreshold(t *testing.T) {
	t.Parallel()
	idle, active := 500*time.Millisecond, 300*time.Millisecond
	first := mrp.BackoffDuration(active, 0, func() float64 { return 1 })
	at := maxScheduleTime(idle, active, first, true, 0)
	if above := maxScheduleTime(idle, active, first+time.Nanosecond, true, 0); at != above {
		t.Errorf("a threshold the wait reaches exactly: %v; one it does not reach: %v — want equal", at, above)
	}
	if below := maxScheduleTime(idle, active, first-time.Nanosecond, true, 0); below <= at {
		t.Errorf("a threshold the wait passes: %v, want more than %v (idle from the second send)", below, at)
	}
}
