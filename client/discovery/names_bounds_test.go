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

// stagedAddresses counts the addresses staged under every host and the
// largest number staged under one.
func stagedAddresses(t *testing.T, n *Names) (total, perHost int) {
	t.Helper()
	onLoop(t, n, func() {
		for _, key := range n.stagedIPs.snapshotKeys() {
			s, _ := n.stagedIPs.get(key)
			total += len(s)
			perHost = max(perHost, len(s))
		}
	})
	return total, perHost
}

// TestNames_StagedAddressesAreWithinTheCacheBound: addresses staged for
// hosts not in the cache count against maxCachedRecords together with the
// cached records, and one host stages at most maxStagedAddressesPerHost —
// whether a sender spreads them over one host or many (review II,
// R2-P3-ctl-lan-01).
func TestNames_StagedAddressesAreWithinTheCacheBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		hosts, perHost int
		host           func(packet, host int) string
	}{
		{name: "one host", hosts: 1, perHost: 50, host: func(int, int) string { return "victim.local" }},
		{name: "many hosts", hosts: 50, perHost: 1, host: func(p, h int) string { return fmt.Sprintf("victim-%d-%d.local", p, h) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, _, _ := testNames(t)
			k := 0
			for p := range 400 {
				answers := []dns.RR{txtRR(fmt.Sprintf("%016X._matterc._udp.local", p), 0xFFFFFFF0, false, "D=1")}
				var extra []dns.RR
				for h := range tc.hosts {
					for range tc.perHost {
						k++
						extra = append(extra, aaaaRR(tc.host(p, h), fmt.Sprintf("fd00::%x:%x", k>>16, k&0xFFFF), 0xFFFFFFF0, false))
					}
				}
				deliver(t, n, 0, answers, extra...)
			}
			staged, perHost := stagedAddresses(t, n)
			var cached int
			onLoop(t, n, func() { cached = len(n.expiring) })
			if staged+cached > maxCachedRecords {
				t.Fatalf("%d staged addresses beside %d cached records — %d in all, past the bound of %d",
					staged, cached, staged+cached, maxCachedRecords)
			}
			if perHost > maxStagedAddressesPerHost {
				t.Fatalf("one host stages %d addresses, want at most %d", perHost, maxStagedAddressesPerHost)
			}
		})
	}
}

// TestNames_StagedHostReplaysAtMostItsBound: an SRV that names a host with
// staged addresses brings at most maxStagedAddressesPerHost of them into
// the cache, so the records discovered before stay discovered (review II,
// R2-P3-ctl-lan-01).
func TestNames_StagedHostReplaysAtMostItsBound(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	const legit = 50
	op := func(i int) string { return fmt.Sprintf("%016X-%016X._matter._tcp.local", 1, i) }
	opHost := func(i int) string { return fmt.Sprintf("op%d.local", i) }
	for i := range legit {
		deliver(t, n, 0, []dns.RR{srvRR(op(i), opHost(i), 5540, 120, true)}, aaaaRR(opHost(i), fmt.Sprintf("fd01::%x", i+1), 120, true))
	}
	for p := range 200 {
		answers := []dns.RR{txtRR(fmt.Sprintf("%016X._matterc._udp.local", p), 0xFFFFFFF0, false, "D=1")}
		extra := make([]dns.RR, 0, 50)
		for i := range 50 {
			extra = append(extra, aaaaRR("victim.local", fmt.Sprintf("fd00::%x:%x", p, i), 0xFFFFFFF0, false))
		}
		deliver(t, n, 0, answers, extra...)
	}
	deliver(t, n, 0, []dns.RR{srvRR(instQ, "victim.local", 5540, 120, true)})
	if got := len(n.Get("victim.local").Records()); got > maxStagedAddressesPerHost {
		t.Fatalf("the SRV brought %d staged addresses into the cache, want at most %d", got, maxStagedAddressesPerHost)
	}
	for i := range legit {
		if !n.Get(opHost(i)).IsDiscovered() {
			t.Fatalf("%s is no longer discovered after the staged host's replay", opHost(i))
		}
	}
}

// TestNames_StagedAddressGoesWhenACachedRecordWould: a staged address is
// dropped at its graced TTL, as a cached record is, not at the next prune
// (review II, R2-P3-ctl-lan-01).
func TestNames_StagedAddressGoesWhenACachedRecordWould(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 0xFFFFFFF0, true, "D=1")}, aRR("other.local", "192.0.2.4", 20, false))
	if staged, _ := stagedAddresses(t, n); staged != 1 {
		t.Fatalf("%d staged addresses, want 1", staged)
	}
	clk.advance(n, time.Duration(float64(20*time.Second)*DefaultTTLGraceFactor)+time.Millisecond)
	if staged, _ := stagedAddresses(t, n); staged != 0 {
		t.Fatalf("%d staged addresses past their graced TTL, before the prune interval %v", staged, stagedIPPruneInterval)
	}
}
