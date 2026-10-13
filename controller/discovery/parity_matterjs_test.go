// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

// Parity with matter.js 0ceb202d: the discovery rules the controller's
// browser mirrors, each case citing the lines it pins.

import (
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestParityDiscovery_RecordKeys pins the cache identity of a record:
// A/AAAA/PTR by type and value, SRV by type and target:port, TXT by type
// and its hex entries sorted; any other type has none.
// matter.js packages/general/src/net/dns-sd/DnssdName.ts:352-377 (keyOf).
func TestParityDiscovery_RecordKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rec  Record
		want string
	}{
		{Record{Type: TypeA, IP: netip.MustParseAddr("192.0.2.1")}, "1 192.0.2.1"},
		{Record{Type: TypeAAAA, IP: netip.MustParseAddr("fd00::1")}, "28 fd00::1"},
		{Record{Type: TypePTR, PTR: "X._matterc._udp.local"}, "12 X._matterc._udp.local"},
		{Record{Type: TypeSRV, SRV: mdns.DNSSRV{Target: "host.local", Port: 5540, Priority: 9}}, "33 host.local:5540"},
		{Record{Type: TypeTXT, TXT: [][]byte{[]byte("D=1"), []byte("CM=1")}}, "16 434d3d31 443d31"},
		{Record{Type: TypeTXT, TXT: [][]byte{[]byte("CM=1"), []byte("D=1")}}, "16 434d3d31 443d31"},
		{Record{Type: TypeA}, ""},
		{Record{Type: mdns.DNSType(dns.TypeNSEC)}, ""},
	}
	for _, c := range cases {
		if got := recordKey(&c.rec); got != c.want {
			t.Errorf("recordKey(%v) = %q, want %q", c.rec.Type, got, c.want)
		}
	}
}

// TestParityDiscovery_TTLRules pins the expiry arithmetic: expiresAt =
// installedAt + round(ttl × 1.05) (DnssdName.ts:25, :156); a TTL under
// 15 s is raised to 15 s (DnssdNames.ts:195-198, defaults :549-552); a
// TTL of 0 is a goodbye that takes effect after the 1 s eviction delay
// (DnssdNames.ts:206-211, DnssdName.ts:202-209, :225-236).
func TestParityDiscovery_TTLRules(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	at := clk.Now()
	deliver(t, n, 0, []dns.RR{
		txtRR("A._matterc._udp.local", 120, false, "D=1"),
		txtRR("B._matterc._udp.local", 5, false, "D=2"),
	})
	a := n.Get("A._matterc._udp.local").Records()[0]
	if a.TTL != 120*time.Second || !a.ExpiresAt.Equal(at.Add(126*time.Second)) {
		t.Errorf("ttl 120: TTL %v expires %v, want 120s at+126s", a.TTL, a.ExpiresAt.Sub(at))
	}
	b := n.Get("B._matterc._udp.local").Records()[0]
	if b.TTL != 15*time.Second || !b.ExpiresAt.Equal(at.Add(15750*time.Millisecond)) {
		t.Errorf("ttl 5: TTL %v expires %v, want raised to 15s, at+15.75s", b.TTL, b.ExpiresAt.Sub(at))
	}

	// Goodbye: the record survives the eviction delay, then goes.
	deliver(t, n, 0, []dns.RR{txtRR("A._matterc._udp.local", 0, false, "D=1")})
	g := n.Get("A._matterc._udp.local").Records()
	if len(g) != 1 || g[0].TTL != time.Second || !g[0].ExpiresAt.Equal(at.Add(time.Second)) {
		t.Fatalf("after goodbye = %+v, want the record retired to 1 s", g)
	}
	clk.advance(n, 999*time.Millisecond)
	if !n.Has("A._matterc._udp.local") {
		t.Fatal("goodbye must not take effect before the eviction delay")
	}
	clk.advance(n, time.Millisecond)
	if n.Has("A._matterc._udp.local") {
		t.Fatal("goodbye must take effect after the eviction delay")
	}
	// A goodbye never extends a record's life (DnssdName.ts:228-230): at
	// at+15.2 s, B has 0.55 s left, less than the delay.
	clk.advance(n, 14200*time.Millisecond)
	deliver(t, n, 0, []dns.RR{txtRR("B._matterc._udp.local", 0, false, "D=2")})
	if r := n.Get("B._matterc._udp.local").Records(); len(r) != 1 || !r[0].ExpiresAt.Equal(at.Add(15750*time.Millisecond)) {
		t.Fatalf("goodbye extended a record: %+v", r)
	}
}

// TestParityDiscovery_CacheFlushRetiresOlderUniqueRecords pins RFC 6762
// §10.2 as matter.js applies it: a cache-flush record of a unique type
// retires the records of its type that arrived before the eviction-delay
// window, after that delay; records inside the window (one record set
// split over packets) survive. DnssdName.ts:148-150, :216-236; DnsCodec.ts
// :198-207 (unique types SRV, TXT, A, AAAA).
func TestParityDiscovery_CacheFlushRetiresOlderUniqueRecords(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aRR(hostQ, "192.0.2.1", 120, true))
	clk.advance(n, 500*time.Millisecond)
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.2", 120, true)) // same window: kept
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.3", 120, true)) // retires both
	if got := len(n.Get(hostQ).Records()); got != 3 {
		t.Fatalf("records right after the flush = %d, want 3 (retired, not yet gone)", got)
	}
	clk.advance(n, time.Second)
	recs := n.Get(hostQ).Records()
	if len(recs) != 1 || recs[0].IP.String() != "192.0.2.3" {
		t.Fatalf("records after the eviction delay = %+v, want only 192.0.2.3", recs)
	}
	// A non-flush record supersedes nothing.
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.4", 120, false))
	clk.advance(n, 2*time.Second)
	if got := len(n.Get(hostQ).Records()); got != 2 {
		t.Fatalf("records = %d, want 2", got)
	}
}

// TestParityDiscovery_UnavailableSRVIgnored pins that an SRV with target
// "" or "." or port 0 designates no service and is not cached.
// DnssdName.ts:125-135, :344-350.
func TestParityDiscovery_UnavailableSRVIgnored(t *testing.T) {
	t.Parallel()
	for _, srv := range []mdns.DNSSRV{{Target: "", Port: 5540}, {Target: ".", Port: 5540}, {Target: "h.local", Port: 0}} {
		if isAvailableService(srv) {
			t.Errorf("isAvailableService(%+v) = true", srv)
		}
	}
	if !isAvailableService(mdns.DNSSRV{Target: "h.local", Port: 1}) {
		t.Error("a target with a port is a service")
	}
	n, _, _ := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, ".", 5540, 120, false), srvRR(instQ, hostQ, 0, 120, false)})
	if n.Has(instQ) {
		t.Fatal("an unavailable SRV must not be cached")
	}
}

// TestParityDiscovery_TXTParameters pins the parameter rules: newest TXT
// record first, an empty entry or an empty key skipped, the split at the
// first "=", the first occurrence of a key winning, a bare key with an
// empty value. DnssdName.ts:76-112.
func TestParityDiscovery_TXTParameters(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, false, "D=1", "OLD=x")})
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, false, "D=2", "", "=nokey", "B64=YQ==", "D=3", "FLAG")})
	p := n.Get(instQ).Parameters()
	want := map[string]string{"D": "2", "B64": "YQ==", "FLAG": "", "OLD": "x"}
	if len(p) != len(want) {
		t.Fatalf("parameters = %q, want %q", p, want)
	}
	for k, v := range want {
		if got, ok := p.Get(k); !ok || got != v {
			t.Errorf("%s = %q (%v), want %q", k, got, ok, v)
		}
	}
	if _, ok := p.Get("missing"); ok {
		t.Error("absent key reported present")
	}
	if got, _ := (Parameters{"DN": []byte{'a', 0xFF, 'b'}}).Get("DN"); got != "a�b" {
		t.Errorf("invalid UTF-8 = %q", got)
	}
}

// TestParityDiscovery_IngressFilter pins which records enter the cache:
// names ending in ._matter._tcp.local, ._matterc._udp.local or
// ._matterd._udp.local (matter.js packages/protocol/src/mdns/MdnsService.ts
// :25-27, :90-95), case-insensitively; the bare service-type name is not
// one of them (a scanner adds its own filter for that).
func TestParityDiscovery_IngressFilter(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"A-B._matter._tcp.local":     true,
		"X._MATTERC._UDP.LOCAL":      true,
		"Y._matterd._udp.local":      true,
		"_matterc._udp.local":        false,
		"printer._ipp._tcp.local":    false,
		"x._matterc._udp.local.evil": false,
	} {
		if got := isMatterServiceRecord(&Record{Name: name}); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}
