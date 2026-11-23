// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestScannerPTRTargetsPastTheBoundAreNotCached: while a Scanner is open,
// a flood of PTRs to fresh instance names creates a cached name only for
// the targets it solicits (at most speculativeTargetMax); the rest are
// not created, so once the PTRs expire and the speculative targets are
// pruned no name without a record stays (review II, R2-C2-bound-2151).
func TestScannerPTRTargetsPastTheBoundAreNotCached(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	var before int
	onLoop(t, n, func() { before = n.names.len() })
	const packets, perPacket = 20, 100
	for p := range packets {
		sub := fmt.Sprintf("_X%d._sub._matterc._udp.local", p)
		ptrs := make([]dns.RR, 0, perPacket)
		for i := range perPacket {
			ptrs = append(ptrs, ptrRR(sub, fmt.Sprintf("T%03d%03d._matterc._udp.local", p, i), 15))
		}
		deliver(t, n, 0, ptrs)
	}
	var flooded, speculative int
	onLoop(t, n, func() { flooded, speculative = n.names.len(), s.speculative.len() })
	if speculative != speculativeTargetMax {
		t.Fatalf("speculative targets %d, want %d", speculative, speculativeTargetMax)
	}
	// The subtype names carrying the PTRs, plus the solicited targets.
	if limit := before + packets + speculativeTargetMax; flooded > limit {
		t.Fatalf("names after the flood %d, want at most %d", flooded, limit)
	}
	clk.advance(n, 2*time.Hour)
	var after int
	onLoop(t, n, func() { after = n.names.len() })
	if after > before {
		t.Fatalf("names after expiry %d, before the flood %d: names without records stay", after, before)
	}
}
