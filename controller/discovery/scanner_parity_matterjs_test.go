// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// A port of matter.js packages/protocol/test/mdns/CommissionableMdnsScannerTest.ts at
// 862ad75f: the scanner's range filter, goodbye handling, TTL eviction,
// concurrent searches, the reported host and the address follow-ups.

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// The constants of CommissionableMdnsScannerTest.ts:25-34.
const (
	cmsServerIPv4 = "10.10.10.1"
	cmsInstanceID = "ABCD1234EFGH5678"
	cmsHostname   = "0011223344550000.local"
	cmsPort       = 5540
	cmsInstanceQ  = cmsInstanceID + "._matterc._udp.local"
)

// cmsRecords is the TXT, SRV and A a test device sends in one response,
// non-cache-flush, each with ttl.
func cmsRecords(ttl uint32, txt ...string) []dns.RR {
	if len(txt) == 0 {
		txt = []string{"D=3840", "CM=1", "VP=4996+22"}
	}
	return []dns.RR{
		txtRR(cmsInstanceQ, ttl, false, txt...),
		srvRR(cmsInstanceQ, cmsHostname, cmsPort, ttl, false),
		aRR(cmsHostname, cmsServerIPv4, ttl, false),
	}
}

type cmsSearch struct {
	found   collector
	done    chan []Device
	errDone chan error
}

// cmsFind runs FindCommissionable on its own goroutine and returns once the
// search is registered with the scanner.
func cmsFind(ctx context.Context, t *testing.T, n *Names, s *Scanner, id Identifier) *cmsSearch {
	t.Helper()
	var before int
	onLoop(t, n, func() { before = len(s.waiters) })
	r := &cmsSearch{done: make(chan []Device, 1), errDone: make(chan error, 1)}
	go func() {
		d, err := s.FindCommissionable(ctx, id, r.found.add)
		r.errDone <- err
		r.done <- d
	}()
	waitFor(t, func() bool {
		var l int
		onLoop(t, n, func() { l = len(s.waiters) })
		return l > before
	})
	return r
}

func (r *cmsSearch) result(t *testing.T) []Device {
	t.Helper()
	if err := <-r.errDone; err != nil {
		t.Fatalf("FindCommissionable: %v", err)
	}
	return <-r.done
}

func (c *collector) snapshot() []Device {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.devices)
}

// answerOn makes the fake responder answer a query that asks name/typ with
// answers, as serverSocket.receipt.on does in the matter.js cases.
func answerOn(t *testing.T, conn *fakeConn, name string, typ mdns.DNSType, answers ...dns.RR) {
	t.Helper()
	var once sync.Once
	conn.mu.Lock()
	conn.onSend = func(msg []byte) {
		m, err := mdns.ParseDNSMessage(msg)
		if err != nil {
			return
		}
		for _, q := range m.Questions {
			if q.Name == name && q.Type == typ {
				once.Do(func() { inject(t, conn, 0, answers) })
				return
			}
		}
	}
	conn.mu.Unlock()
}

func TestCommissionableMdnsScanner(t *testing.T) {
	t.Parallel()

	byLong := ByLongDiscriminator(3840)

	t.Run("ignores a device whose discriminator or commissioning mode is out of range", func(t *testing.T) {
		t.Parallel()
		n, _, _ := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		// CommissionableMdnsScannerTest.ts:131-136.
		txts := []struct {
			instance string
			txt      []string
		}{
			{"VALID00000000000", []string{"D=4095", "CM=255"}},
			{"BIGDISCRIMINATOR", []string{"D=4096", "CM=1"}},
			{"BIGCOMMISSIONMOD", []string{"D=3840", "CM=256"}},
			{"EMPTYDISCRIMINAT", []string{"D=", "CM=1"}},
		}
		answers := make([]dns.RR, 0, 2*len(txts))
		for _, e := range txts {
			q := e.instance + "._matterc._udp.local"
			answers = append(answers, txtRR(q, 120, false, e.txt...), srvRR(q, cmsHostname, cmsPort, 120, false))
		}
		deliver(t, n, 0, answers, aRR(cmsHostname, cmsServerIPv4, 120, false))
		var found []string
		for _, e := range txts {
			for _, d := range s.Discovered(ByInstanceID(e.instance)) {
				found = append(found, d.InstanceID)
				if d.D != 4095 || d.CM != 255 {
					t.Errorf("%s: D=%d CM=%d", d.InstanceID, d.D, d.CM)
				}
			}
		}
		if !equalStrings(found, []string{"VALID00000000000"}) {
			t.Errorf("found %v, want only VALID00000000000", found)
		}
	})

	t.Run("ignores TTL=0 goodbye within protection window", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Fatalf("discovered %d, want 1", got)
		}
		deliver(t, n, 0, []dns.RR{srvRR(cmsInstanceQ, cmsHostname, cmsPort, 0, false)})
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Errorf("after the goodbye: discovered %d, want 1 (the goodbye waits out the eviction delay)", got)
		}
	})

	t.Run("removes a device whose goodbye follows a re-announcement, and finds it again when it returns", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Fatalf("discovered %d, want 1", got)
		}
		clk.advance(n, time.Minute)
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		deliver(t, n, 0, []dns.RR{
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 0, false),
			txtRR(cmsInstanceQ, 0, false, "D=3840", "CM=1", "VP=4996+22"),
		})
		clk.advance(n, time.Second)
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 0 {
			t.Fatalf("after the goodbye: discovered %d, want 0", got)
		}
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Errorf("after it returned: discovered %d, want 1", got)
		}
	})

	t.Run("re-discovers device after TTL expiry eviction", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		onLoop(t, n, func() { n.minTTL = 0 }) // minTtl: Millis(0), :422
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, cmsRecords(2))
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Fatalf("discovered %d, want 1", got)
		}
		clk.advance(n, 3*time.Second)
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 0 {
			t.Fatalf("after the TTL: discovered %d, want 0", got)
		}
		deliver(t, n, 0, cmsRecords(2))
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Errorf("re-advertised: discovered %d, want 1", got)
		}
	})

	t.Run("concurrent discoveries time out with empty results when no device is present", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		// The matter.js timeouts (500 ms, 1000 ms, :488-489) are the
		// caller's context in Go; they run on the fake clock.
		id := ByLongDiscriminator(1234)
		shortCtx, shortCancel := context.WithCancel(t.Context())
		defer shortCancel()
		longCtx, longCancel := context.WithCancel(t.Context())
		defer longCancel()
		clk.AfterFunc(500*time.Millisecond, shortCancel)
		clk.AfterFunc(1000*time.Millisecond, longCancel)
		short := cmsFind(shortCtx, t, n, s, id)
		long := cmsFind(longCtx, t, n, s, id)
		clk.advance(n, time.Second)
		if got := short.result(t); len(got) != 0 {
			t.Errorf("short = %+v, want []", got)
		}
		if got := long.result(t); len(got) != 0 {
			t.Errorf("long = %+v, want []", got)
		}
	})

	t.Run("two concurrent discoveries both resolve when device is found", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		first := cmsFind(t.Context(), t, n, s, byLong)
		second := cmsFind(t.Context(), t, n, s, byLong)
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		waitFor(t, func() bool { return first.found.len() == 1 && second.found.len() == 1 })
		s.Cancel(byLong)
		r1, r2 := first.result(t), second.result(t)
		for i, found := range [][]Device{first.found.snapshot(), second.found.snapshot()} {
			if len(found) != 1 || found[0].InstanceID != cmsInstanceID {
				t.Errorf("search %d found %+v", i+1, found)
			}
		}
		if len(r1) != 1 || len(r2) != 1 {
			t.Errorf("results %d and %d, want 1 and 1", len(r1), len(r2))
		}
	})

	t.Run("finds devices by long discriminator", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, cmsRecords(120))
		clk.advance(n, 10*time.Millisecond)
		results := s.Discovered(byLong)
		if len(results) != 1 || results[0].D != 3840 {
			t.Errorf("results = %+v", results)
		}
		if got := s.Discovered(ByLongDiscriminator(1234)); len(got) != 0 {
			t.Errorf("1234 = %+v, want none", got)
		}
	})

	t.Run("reports the host its SRV record names", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, cmsRecords(120, "D=3840", "CM=1"))
		clk.advance(n, 10*time.Millisecond)
		got := s.Discovered(AnyCommissionable())
		if len(got) != 1 || got[0].Hostname != "0011223344550000" {
			t.Errorf("devices = %+v", got)
		}
	})

	t.Run("reports the host of an SRV record that arrives after the device was cached", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		// The TXT alone identifies the device, so it is cached first.
		deliver(t, n, 0, []dns.RR{txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1")})
		clk.advance(n, 10*time.Millisecond)
		onLoop(t, n, func() {
			if !s.cache.has(lowerOf(cmsInstanceQ)) {
				t.Error("the TXT alone must cache the device")
			}
		})
		deliver(t, n, 0, []dns.RR{
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
			aRR(cmsHostname, cmsServerIPv4, 120, false),
		})
		clk.advance(n, 10*time.Millisecond)
		got := s.Discovered(AnyCommissionable())
		if len(got) != 1 || got[0].Hostname != "0011223344550000" {
			t.Errorf("devices = %+v", got)
		}
	})

	t.Run("reports no host while two SRV records disagree about it, rather than the one it left", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		announce := func(target string) {
			deliver(t, n, 0, []dns.RR{
				txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1"),
				srvRR(cmsInstanceQ, target, cmsPort, 120, false),
				aRR(target, cmsServerIPv4, 120, false),
			})
			clk.advance(n, 10*time.Millisecond)
		}
		announce(cmsHostname)
		announce("0011223344551111.local")
		got := s.Discovered(AnyCommissionable())
		if len(got) != 1 || got[0].Hostname != "" {
			t.Errorf("devices = %+v, want one without a host", got)
		}
	})

	t.Run("reports the host where the device answers on two ports of it", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, []dns.RR{
			txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1"),
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
			srvRR(cmsInstanceQ, cmsHostname, cmsPort+1, 120, false),
			aRR(cmsHostname, cmsServerIPv4, 120, false),
		})
		clk.advance(n, 10*time.Millisecond)
		got := s.Discovered(AnyCommissionable())
		if len(got) != 1 || got[0].Hostname != "0011223344550000" {
			t.Errorf("devices = %+v", got)
		}
	})

	t.Run("defers delivery until A/AAAA records arrive", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		search := cmsFind(t.Context(), t, n, s, byLong)
		deliver(t, n, 0, []dns.RR{
			txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1", "VP=4996+22"),
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
		})
		clk.advance(n, 10*time.Millisecond)
		if search.found.len() != 0 || len(s.Discovered(byLong)) != 0 {
			t.Fatal("delivered without an address")
		}
		// The A rides along a filtered PTR, :915-935.
		deliver(t, n, 0, []dns.RR{ptrRR("_matterc._udp.local", cmsInstanceQ, 120)}, aRR(cmsHostname, cmsServerIPv4, 120, false))
		clk.advance(n, 10*time.Millisecond)
		waitFor(t, func() bool { return search.found.len() == 1 })
		if f := search.found.snapshot(); f[0].InstanceID != cmsInstanceID || len(f[0].Addresses) == 0 {
			t.Errorf("found %+v", f[0])
		}
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Errorf("discovered %d, want 1", got)
		}
		s.Cancel(byLong)
		search.result(t)
	})

	t.Run("PTR-follow solicits SRV/TXT when PTR arrives without additional records", func(t *testing.T) {
		t.Parallel()
		n, conn, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		search := cmsFind(t.Context(), t, n, s, byLong)
		answerOn(t, conn, cmsInstanceQ, TypeSRV,
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
			txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1", "VP=4996+22"),
			aRR(cmsHostname, cmsServerIPv4, 120, false))
		deliver(t, n, 0, []dns.RR{ptrRR("_matterc._udp.local", cmsInstanceQ, 120)})
		clk.advance(n, 10*time.Millisecond)
		waitFor(t, func() bool {
			clk.advance(n, time.Second)
			return search.found.len() == 1
		})
		if f := search.found.snapshot(); f[0].InstanceID != cmsInstanceID || f[0].D != 3840 || len(f[0].Addresses) == 0 {
			t.Errorf("found %+v", f[0])
		}
		s.Cancel(byLong)
		search.result(t)
	})

	t.Run("discovers device when A/AAAA arrives before SRV in separate messages", func(t *testing.T) {
		t.Parallel()
		n, _, clk := testNames(t)
		s := NewScanner(n)
		defer s.Close()
		search := cmsFind(t.Context(), t, n, s, byLong)
		// The PTR carries the A along; the A is staged for a host no SRV
		// names yet.
		deliver(t, n, 0, []dns.RR{ptrRR("_matterc._udp.local", cmsInstanceQ, 120), aRR(cmsHostname, cmsServerIPv4, 120, false)})
		clk.advance(n, 10*time.Millisecond)
		if search.found.len() != 0 {
			t.Fatal("delivered from a PTR alone")
		}
		deliver(t, n, 0, []dns.RR{
			txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1", "VP=4996+22"),
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
		})
		clk.advance(n, 10*time.Millisecond)
		waitFor(t, func() bool { return search.found.len() == 1 })
		f := search.found.snapshot()[0]
		if f.InstanceID != cmsInstanceID || !slices.Contains(addrStrings(f.Addresses), cmsServerIPv4+":5540") {
			t.Errorf("found %+v, want the staged %s", f, cmsServerIPv4)
		}
		s.Cancel(byLong)
		search.result(t)
	})

	t.Run("re-solicits A/AAAA when cached entry's addresses expired before next discovery", func(t *testing.T) {
		t.Parallel()
		n, conn, clk := testNames(t)
		onLoop(t, n, func() { n.minTTL = 0 }) // minTtl: Millis(0), :1156
		s := NewScanner(n)
		defer s.Close()
		deliver(t, n, 0, []dns.RR{
			txtRR(cmsInstanceQ, 120, false, "D=3840", "CM=1", "VP=4996+22"),
			srvRR(cmsInstanceQ, cmsHostname, cmsPort, 120, false),
			aRR(cmsHostname, cmsServerIPv4, 2, false),
		})
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 1 {
			t.Fatalf("discovered %d, want 1", got)
		}
		clk.advance(n, 3*time.Second)
		clk.advance(n, 10*time.Millisecond)
		if got := len(s.Discovered(byLong)); got != 0 {
			t.Fatalf("after the A's TTL: discovered %d, want 0", got)
		}
		answerOn(t, conn, cmsHostname, TypeA, aRR(cmsHostname, cmsServerIPv4, 120, false))
		search := cmsFind(t.Context(), t, n, s, byLong)
		waitFor(t, func() bool { return search.found.len() == 1 })
		if f := search.found.snapshot()[0]; f.InstanceID != cmsInstanceID || len(f.Addresses) == 0 {
			t.Errorf("found %+v", f)
		}
		s.Cancel(byLong)
		search.result(t)
	})
}
