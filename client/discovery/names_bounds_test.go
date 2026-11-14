// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestNames_TTLHasACeiling: a record is cached for at most the ceiling,
// whatever TTL its sender states (review II, R2-P2-alloc-03).
func TestNames_TTLHasACeiling(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 0xFFFFFFF0, false, "D=1")})
	recs := n.Get(instQ).Records()
	if len(recs) != 1 || recs[0].TTL != maxRecordTTL {
		t.Fatalf("records %+v, want one with TTL %v", recs, maxRecordTTL)
	}
	clk.advance(n, time.Duration(float64(maxRecordTTL)*DefaultTTLGraceFactor)+time.Second)
	if n.Has(instQ) {
		t.Fatal("the record outlived the ceiling")
	}
}

// TestNames_CachedRecordsAreBounded: the cache holds at most
// maxCachedRecords records; past it the record installed or refreshed
// longest ago goes, with its name once nothing keeps it (review II,
// R2-P2-alloc-03).
func TestNames_CachedRecordsAreBounded(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	const perPacket = 50
	inst := func(i int) string { return fmt.Sprintf("%016X._matterc._udp.local", i) }
	host := func(i int) string { return fmt.Sprintf("h%d.local", i) }
	total, last := maxCachedRecords+2*perPacket, 0
	for first := 0; first < total; first += perPacket {
		var answers, extra []dns.RR
		for i := first; i < first+perPacket; i++ {
			answers = append(answers, srvRR(inst(i), host(i), 5540, 0xFFFFFFF0, false))
			extra = append(extra, aaaaRR(host(i), fmt.Sprintf("fd00::%x", i+1), 0xFFFFFFF0, false))
			last = i
		}
		deliver(t, n, 0, answers, extra...)
	}
	var cached, names int
	onLoop(t, n, func() { cached, names = len(n.expiring), n.names.len() })
	if cached > maxCachedRecords {
		t.Fatalf("%d records cached, want at most %d", cached, maxCachedRecords)
	}
	if names > 2*maxCachedRecords {
		t.Fatalf("%d names cached", names)
	}
	if n.Has(inst(0)) || n.Has(host(0)) {
		t.Fatal("the oldest record and its target were kept")
	}
	if !n.Has(inst(last)) || !n.Get(host(last)).IsDiscovered() {
		t.Fatal("the newest record or its target is missing")
	}
}
