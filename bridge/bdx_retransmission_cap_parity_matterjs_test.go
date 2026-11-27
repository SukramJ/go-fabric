// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"
)

// retransmissionsWithin is MessageExchangeTest.ts:428-482 at db91886c on the
// bridge's reliable tracker: one message of an exchange of this node's,
// tracked with margin on the base interval base, its retransmissions
// counted over window in 500 ms ticks of the schedule's clock.
func retransmissionsWithin(t *testing.T, bdxExchange bool, base, margin, window time.Duration) int {
	t.Helper()
	tr := newOutboundReliableTracker(func(uint16, time.Time) time.Duration { return base }, nil)
	start := time.UnixMilli(1_700_000_000_000)
	dest := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	kick := &exchangeKick{bdx: bdxExchange, ended: func() bool { return false }}
	if err := tr.trackKickable(1, 0x0E40, 0x0201, true, []byte{1}, dest, start, margin, kick); err != nil {
		t.Fatal(err)
	}
	tr.markSent(0x0E40, 1, start)
	sends := 0
	for elapsed := 500 * time.Millisecond; elapsed <= window; elapsed += 500 * time.Millisecond {
		tr.Tick(start.Add(elapsed), func(net.Addr, []byte) error { sends++; return nil })
	}
	return sends
}

// TestBDXRetransmissionIntervalCap ports matter.js
// packages/protocol/test/protocol/MessageExchangeTest.ts:420-531 at
// db91886c (describe "BDX retransmission interval cap"). PADDED's local
// additional delay of 20 s is the exchange's margin here, FAST_PEER's and
// SLEEPY's schedule the base interval the session resolver states: the
// tracker selects active or idle itself, so SLEEPY (threshold 0, nominally
// idle) is its 20 s idle interval.
func TestBDXRetransmissionIntervalCap(t *testing.T) {
	t.Parallel()
	const window = 8 * time.Second
	fast, sleepy, padded := 500*time.Millisecond, 20*time.Second, 20*time.Second
	for _, tc := range []struct {
		name   string
		bdx    bool
		base   time.Duration
		margin time.Duration
		want   int
	}{
		{"retransmits a BDX message within the peer's response budget", true, fast, padded, 1},
		{"leaves a non-BDX schedule uncapped", false, fast, padded, 0},
		{"caps a slow peer's idle cadence too, since a peer mid-transfer is not idle", true, sleepy, 0, 1},
		{"leaves a non-BDX slow peer on its idle cadence", false, sleepy, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := retransmissionsWithin(t, tc.bdx, tc.base, tc.margin, window); got != tc.want {
				t.Fatalf("retransmissions within %v = %d, want %d", window, got, tc.want)
			}
		})
	}
}
