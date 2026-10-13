// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	cInst = "1A2B3C4D5E6F7788"
	cQ    = cInst + "._matterc._udp.local"
	cHost = "AABBCCDDEEFF0000.local"
)

// announce is the full answer a commissionable node gives to a PTR query.
func announce(inst, host, d, cm string, ips ...string) (answers, extra []dns.RR) {
	q := inst + "._matterc._udp.local"
	answers = []dns.RR{ptrRR("_matterc._udp.local", q, 4500)}
	extra = []dns.RR{
		srvRR(q, host, 5540, 120, true),
		txtRR(q, 4500, true, "D="+d, "CM="+cm, "VP=65521+32769", "DT=257", "DN=Lamp", "SII=500", "SAI=300", "PH=33"),
	}
	for _, ip := range ips {
		if !containsColon(ip) {
			extra = append(extra, aRR(host, ip, 120, true))
		} else {
			extra = append(extra, aaaaRR(host, ip, 120, true))
		}
	}
	return answers, extra
}

func containsColon(s string) bool {
	for i := range len(s) {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

type collector struct {
	mu      sync.Mutex
	devices []Device
}

func (c *collector) add(d Device) {
	c.mu.Lock()
	c.devices = append(c.devices, d)
	c.mu.Unlock()
}

func (c *collector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.devices)
}

func TestScanner_FindByLongDiscriminator(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got collector
	type res struct {
		devices []Device
		err     error
	}
	done := make(chan res, 1)
	go func() {
		d, err := s.FindCommissionable(ctx, ByLongDiscriminator(3840), got.add)
		done <- res{d, err}
	}()
	waitFor(t, func() bool { return len(conn.queries(t)) >= 1 })
	q := conn.queries(t)[0]
	if want := []string{"_matterc._udp.local/PTR", "_L3840._sub._matterc._udp.local/PTR"}; !equalStrings(questionsOf(q), want) {
		t.Fatalf("first query = %v, want %v in one packet", questionsOf(q), want)
	}
	answers, extra := announce(cInst, cHost, "3840", "1", "192.0.2.10", "fe80::10")
	inject(t, conn, 7, answers, extra...)
	waitFor(t, func() bool { return got.len() == 1 })
	cancel()
	r := <-done
	if r.err != nil || len(r.devices) != 1 {
		t.Fatalf("result = %+v", r)
	}
	d := r.devices[0]
	if d.InstanceID != cInst || d.D != 3840 || d.CM != 1 || d.Hostname != "AABBCCDDEEFF0000" {
		t.Fatalf("device = %+v", d)
	}
	if got := addrStrings(d.Addresses); !equalStrings(got, []string{"[fe80::10%eth7]:5540", "192.0.2.10:5540"}) {
		t.Fatalf("addresses = %v", got)
	}
	if *d.VP != "65521+32769" || *d.DT != 257 || *d.DN != "Lamp" || *d.PH != 33 || *d.ICD != 0 || *d.T != (SupportedTransports{}) {
		t.Fatalf("TXT data = %+v", d.TXTData)
	}
	// The discovery stopped with the search.
	before := len(conn.queries(t))
	if len(s.Discovered(ByLongDiscriminator(3840))) != 1 || len(s.Discovered(ByLongDiscriminator(1))) != 0 {
		t.Fatal("Discovered must report the cached match only")
	}
	if len(conn.queries(t)) != before {
		t.Fatal("Discovered must not query")
	}
}

func TestScanner_CachedMatchesFirstAndCancelInCallbackSkipsTheNetwork(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	answers, extra := announce(cInst, cHost, "3840", "2", "192.0.2.10")
	// Arrives before anyone searched: picked up from the cache.
	inject(t, conn, 0, answers, extra...)
	waitFor(t, func() bool { return n.Has(cQ) })
	devices, err := s.FindCommissionable(t.Context(), AnyCommissionable(), func(Device) { s.Cancel(AnyCommissionable()) })
	if err != nil || len(devices) != 1 || devices[0].CM != 2 {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	if len(conn.queries(t)) != 0 {
		t.Fatalf("a search cancelled by its callback on a cached match must not query, sent %d", len(conn.queries(t)))
	}
	// A cancelled context after a cached match ends the same way.
	ctx, cancel := context.WithCancel(t.Context())
	devices, _ = s.FindCommissionable(ctx, ByShortDiscriminator(15), func(Device) { cancel() })
	if len(devices) != 1 || len(conn.queries(t)) != 0 {
		t.Fatalf("devices = %d, queries = %d", len(devices), len(conn.queries(t)))
	}
}

func TestScanner_FollowsPointersSpeculatively(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got collector
	go func() { _, _ = s.FindCommissionable(ctx, AnyCommissionable(), got.add) }()
	waitFor(t, func() bool { return asked(t, conn, "_CM._sub._matterc._udp.local", TypePTR) })
	// A bare PTR: the target's SRV and TXT are solicited.
	inject(t, conn, 0, []dns.RR{ptrRR("_CM._sub._matterc._udp.local", cQ, 4500)})
	waitFor(t, func() bool { return asked(t, conn, cQ, TypeSRV) && asked(t, conn, cQ, TypeTXT) })
	onLoop(t, n, func() {
		if !s.speculative.has(lowerOf(cQ)) {
			t.Error("target must be tracked speculatively")
		}
	})
	// The SRV alone: the device cannot be built yet, TXT is asked for.
	conn.reset()
	inject(t, conn, 0, []dns.RR{srvRR(cQ, cHost, 5540, 120, true)})
	waitFor(t, func() bool { return asked(t, conn, cQ, TypeTXT) })
	onLoop(t, n, func() {
		if s.speculative.has(lowerOf(cQ)) {
			t.Error("a discovered instance leaves the speculative set")
		}
		if s.cache.has(lowerOf(cQ)) || len(s.pending) != 1 {
			t.Error("without TXT the instance waits")
		}
	})
	// The TXT: cached, but no address yet, so A/AAAA is asked for.
	conn.reset()
	inject(t, conn, 0, []dns.RR{txtRR(cQ, 4500, true, "D=100", "CM=1")})
	waitFor(t, func() bool { return asked(t, conn, cHost, TypeA) && asked(t, conn, cHost, TypeAAAA) })
	if got.len() != 0 {
		t.Fatal("no address, no delivery")
	}
	inject(t, conn, 0, []dns.RR{aaaaRR(cHost, "fd00::10", 120, true)})
	waitFor(t, func() bool { return got.len() == 1 })
	// Further address changes do not deliver again.
	inject(t, conn, 0, []dns.RR{aRR(cHost, "192.0.2.10", 120, true)})
	clk.advance(n, 0)
	if got.len() != 1 {
		t.Fatalf("delivered %d times", got.len())
	}
}

func TestScanner_InvalidTXTNeverBuildsAndNameExpiryDropsTheCache(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	answers, extra := announce("BAD", cHost, "0abc", "1", "192.0.2.10")
	inject(t, conn, 0, answers, extra...)
	answers, extra = announce(cInst, cHost, "3840", "1", "192.0.2.10")
	inject(t, conn, 0, answers, extra...)
	waitFor(t, func() bool { return n.Has(cQ) })
	if _, err := s.FindCommissionable(cancelledContext(), AnyCommissionable(), nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Discovered(AnyCommissionable()); len(got) != 1 || got[0].InstanceID != cInst {
		t.Fatalf("discovered = %+v", got)
	}
	onLoop(t, n, func() {
		// The invalid instance waits for a valid TXT. matter.js's search
		// re-runs #onDiscovered over every discovered name
		// (CommissionableMdnsScanner.ts:173-175), and an instance that is
		// not cached gets a further IpService and pending observer each
		// time (:344-376): one from the announcement, one from the search.
		if len(s.pending) != 2 {
			t.Errorf("pending = %d, want 2", len(s.pending))
		}
	})
	// The node goes away (goodbyes); its cached device goes with its name.
	inject(t, conn, 0, nil, srvRR(cQ, cHost, 5540, 0, false), txtRR(cQ, 0, false, "D=3840", "CM=1", "VP=65521+32769", "DT=257", "DN=Lamp", "SII=500", "SAI=300", "PH=33"),
		srvRR("BAD._matterc._udp.local", cHost, 5540, 0, false), txtRR("BAD._matterc._udp.local", 0, false, "D=0abc", "CM=1", "VP=65521+32769", "DT=257", "DN=Lamp", "SII=500", "SAI=300", "PH=33"))
	waitFor(t, func() bool {
		clk.advance(n, 2*time.Second)
		return len(s.Discovered(AnyCommissionable())) == 0
	})
	onLoop(t, n, func() {
		if s.cache.len() != 0 || len(s.pending) != 0 {
			t.Errorf("cache = %d pending = %d", s.cache.len(), len(s.pending))
		}
	})
}

func TestScanner_CachedWithoutAddressIsSolicitedOnSearch(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	answers, extra := announce(cInst, cHost, "3840", "1", "192.0.2.10")
	// The A expires before the SRV and TXT.
	extra[len(extra)-1] = aRR(cHost, "192.0.2.10", 20, true)
	inject(t, conn, 0, answers, extra...)
	waitFor(t, func() bool { return n.Has(cQ) })
	clk.advance(n, 25*time.Second)
	if len(s.Discovered(AnyCommissionable())) != 0 {
		t.Fatal("no address, not discovered")
	}
	conn.reset()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got collector
	go func() { _, _ = s.FindCommissionable(ctx, ByVendorProduct(65521, 32769), got.add) }()
	waitFor(t, func() bool { return asked(t, conn, cHost, TypeA) })
	inject(t, conn, 0, []dns.RR{aRR(cHost, "192.0.2.11", 120, true)})
	waitFor(t, func() bool { return got.len() == 1 })
}

func TestScanner_CancelCloseAndClosedCache(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := NewScanner(n)
	done := make(chan error, 2)
	go func() { _, err := s.FindCommissionable(t.Context(), ByDeviceType(257), nil); done <- err }()
	go func() { _, err := s.FindCommissionable(t.Context(), ByProduct(32769), nil); done <- err }()
	waitFor(t, func() bool {
		return asked(t, conn, "_T257._sub._matterc._udp.local", TypePTR) && asked(t, conn, "_CM._sub._matterc._udp.local", TypePTR)
	})
	s.Cancel(ByDeviceType(257))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if err := <-done; err != nil {
		t.Fatalf("close ends a search without error: %v", err)
	}

	s2 := NewScanner(n)
	go func() { _, err := s2.FindCommissionable(t.Context(), ByInstanceID(cInst), nil); done <- err }()
	waitFor(t, func() bool { return asked(t, conn, cQ, TypePTR) })
	_ = n.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("closed cache: %v", err)
	}
	s3 := NewScanner(n)
	if _, err := s3.FindCommissionable(t.Context(), AnyCommissionable(), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("scanner on a closed cache: %v", err)
	}
	if s3.Discovered(AnyCommissionable()) != nil {
		t.Fatal("closed: nothing discovered")
	}
}

func TestScanner_SpeculativeTargetsAreBoundedAndPruned(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	ptrs := make([]dns.RR, 0, speculativeTargetMax+6)
	for i := range speculativeTargetMax + 5 {
		ptrs = append(ptrs, ptrRR("_matterc._udp.local", "I"+string(rune('A'+i/26))+string(rune('A'+i%26))+"._matterc._udp.local", 4500))
	}
	ptrs = append(ptrs, ptrRR("_matterc._udp.local", "_bogus._matterc._udp.local", 4500))
	inject(t, conn, 0, ptrs)
	waitFor(t, func() bool {
		var l int
		onLoop(t, n, func() { l = s.speculative.len() })
		return l == speculativeTargetMax
	})
	// Pruning forgets a target solicited for an hour, not one younger.
	onLoop(t, n, func() {
		now := n.clock.Now()
		for i, key := range s.speculative.snapshotKeys() {
			st, _ := s.speculative.get(key)
			st.createdAt = now.Add(-speculativeTargetTTL)
			if i == 0 {
				st.createdAt = now.Add(-speculativeTargetTTL + time.Second)
			}
		}
		s.pruneSpeculativeTargets()
		if s.speculative.len() != 1 {
			t.Errorf("speculative after pruning = %d, want 1", s.speculative.len())
		}
	})
	_ = clk
}

func TestScanner_CleanupTimerRunsEveryHalfHour(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	onLoop(t, n, func() {
		cancelled := false
		s.speculative.set("x._matterc._udp.local", &speculativeTarget{cancel: func() { cancelled = true }, createdAt: n.clock.Now()})
		_ = cancelled
	})
	clk.advance(n, 30*time.Minute)
	onLoop(t, n, func() {
		if s.speculative.len() != 1 {
			t.Error("a target younger than an hour stays")
		}
	})
	clk.advance(n, 30*time.Minute)
	onLoop(t, n, func() {
		if s.speculative.len() != 0 {
			t.Error("the cleanup timer must prune an hour-old target")
		}
	})
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func lowerOf(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
