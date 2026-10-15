// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"bytes"
	"net"
	"net/netip"
	"testing"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestPackDNSQuery_RoundTrip: a query with every record type as known
// answers parses back to the same records, raw TXT bytes and names
// included, with the cache-flush bit cleared.
func TestPackDNSQuery_RoundTrip(t *testing.T) {
	known := []mdns.DNSRecord{
		{Name: "h.local", Type: mdns.DNSTypeA, TTL: 120, CacheFlush: true, IP: netip.MustParseAddr("192.0.2.1")},
		{Name: "h.local", Type: mdns.DNSTypeAAAA, TTL: 120, IP: netip.MustParseAddr("fe80::1")},
		{Name: "_matterc._udp.local", Type: mdns.DNSTypePTR, TTL: 4500, PTR: "AB._matterc._udp.local"},
		{Name: "AB._matterc._udp.local", Type: mdns.DNSTypeSRV, TTL: 120, SRV: mdns.DNSSRV{Target: "h.local", Port: 5540, Priority: 1, Weight: 2}},
		{Name: "AB._matterc._udp.local", Type: mdns.DNSTypeTXT, TTL: 4500, TXT: [][]byte{[]byte("DN=a\"b\\c"), {'X', '=', 0xC3, 0xA4, 0x01}}},
		{Name: "h.local", Type: mdns.DNSTypeA, TTL: 1, IP: netip.MustParseAddr("fd00::1")}, // wrong family: dropped
		{Name: "h.local", Type: mdns.DNSTypeAAAA, TTL: 1, IP: netip.MustParseAddr("192.0.2.9")},
		{Name: "h.local", Type: mdns.DNSType(dns.TypeNSEC), TTL: 1},
	}
	b, err := mdns.PackDNSQuery([]mdns.DNSQuestion{{Name: "_matterc._udp.local", Type: mdns.DNSTypePTR}, {Name: "h.local.", Type: mdns.DNSTypeAAAA}}, known)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mdns.ParseDNSMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Response || len(m.Questions) != 2 || m.Questions[1].Name != "h.local" || m.Questions[1].Type != mdns.DNSTypeAAAA {
		t.Fatalf("questions = %+v", m.Questions)
	}
	if len(m.Answers) != 5 {
		t.Fatalf("answers = %d, want 5", len(m.Answers))
	}
	for i, r := range m.Answers {
		w := known[i]
		if r.Name != w.Name || r.Type != w.Type || r.TTL != w.TTL || r.CacheFlush || r.IP != w.IP || r.PTR != w.PTR || r.SRV != w.SRV || len(r.TXT) != len(w.TXT) {
			t.Errorf("answer %d = %+v, want %+v (no cache flush)", i, r, w)
		}
		for j := range r.TXT {
			if !bytes.Equal(r.TXT[j], w.TXT[j]) {
				t.Errorf("TXT[%d] = %q, want %q", j, r.TXT[j], w.TXT[j])
			}
		}
	}
	if _, err := mdns.ParseDNSMessage([]byte{1}); err == nil {
		t.Fatal("garbage must not parse")
	}
	if _, err := mdns.PackDNSQuery([]mdns.DNSQuestion{{Name: string(bytes.Repeat([]byte("a"), 300)), Type: mdns.DNSTypeA}}, nil); err == nil {
		t.Fatal("an over-long label must not pack")
	}
}

// TestParseDNSMessage_ResponseSections: the sections, the cache-flush bit
// and the root SRV target ("." → "").
func TestParseDNSMessage_ResponseSections(t *testing.T) {
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{&dns.SRV{Hdr: dns.RR_Header{Name: "x._matterc._udp.local.", Rrtype: dns.TypeSRV, Class: dns.ClassINET | 0x8000, Ttl: 120}, Target: ".", Port: 1}}
	m.Ns = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "h.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 5}, A: net.IPv4(192, 0, 2, 1)}}
	m.Extra = []dns.RR{&dns.TXT{Hdr: dns.RR_Header{Name: `we\.ird.local.`, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 5}, Txt: []string{`a\255`}}}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	got, err := mdns.ParseDNSMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Response || !got.Answers[0].CacheFlush || got.Answers[0].SRV.Target != "" {
		t.Fatalf("answer = %+v", got.Answers[0])
	}
	if len(got.Authorities) != 1 || got.Authorities[0].IP.String() != "192.0.2.1" {
		t.Fatalf("authorities = %+v", got.Authorities)
	}
	if got.Additional[0].Name != "we.ird.local" || !bytes.Equal(got.Additional[0].TXT[0], []byte{'a', 0xFF}) {
		t.Fatalf("additional = %+v", got.Additional[0])
	}
}

func TestDNSType_String(t *testing.T) {
	for typ, want := range map[mdns.DNSType]string{mdns.DNSTypeA: "A", mdns.DNSTypePTR: "PTR", mdns.DNSTypeTXT: "TXT", mdns.DNSTypeAAAA: "AAAA", mdns.DNSTypeSRV: "SRV", 65000: "TYPE65000"} {
		if got := typ.String(); got != want {
			t.Errorf("%d: %q, want %q", typ, got, want)
		}
	}
}

// TestOpenQueryConn opens the production transport where the host allows
// a multicast join.
func TestOpenQueryConn(t *testing.T) {
	c, err := mdns.OpenQueryConn()
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	if c.Responses() == nil {
		t.Fatal("no response channel")
	}
	_ = c.Close()
	_ = c.Close()
}
