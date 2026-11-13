// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
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
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: ":0", SessionIntervals: tc.local}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := b.chunkStatusResponseTimeout(0)
			if diff := got - tc.want; diff < -time.Millisecond || diff > time.Millisecond {
				t.Fatalf("chunk StatusResponse bound = %v, want %v", got, tc.want)
			}
		})
	}
}
