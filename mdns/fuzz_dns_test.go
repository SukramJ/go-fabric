// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"net/netip"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// FuzzParseDNSMessage feeds ParseDNSMessage the datagrams any host on the
// link can multicast. The browser parses every one of them. Invariant: it
// parses or returns an error and never panics. A parsed message holds no
// more records and questions than the input has bytes, so a short packet
// cannot claim a large allocation.
func FuzzParseDNSMessage(f *testing.F) {
	known := []mdns.DNSRecord{
		{Name: "h.local", Type: mdns.DNSTypeA, TTL: 120, IP: netip.MustParseAddr("192.0.2.1")},
		{Name: "h.local", Type: mdns.DNSTypeAAAA, TTL: 120, IP: netip.MustParseAddr("fe80::1")},
		{Name: "_matterc._udp.local", Type: mdns.DNSTypePTR, TTL: 4500, PTR: "AB._matterc._udp.local"},
		{Name: "AB._matterc._udp.local", Type: mdns.DNSTypeSRV, TTL: 120, SRV: mdns.DNSSRV{Target: "h.local", Port: 5540}},
		{Name: "AB._matterc._udp.local", Type: mdns.DNSTypeTXT, TTL: 4500, TXT: [][]byte{[]byte("D=3840"), []byte("CM=1")}},
	}
	b, err := mdns.PackDNSQuery([]mdns.DNSQuestion{{Name: "_matterc._udp.local", Type: mdns.DNSTypePTR}}, known)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Add([]byte{})
	f.Add([]byte{1})
	f.Add(make([]byte, 12))
	f.Fuzz(func(t *testing.T, msg []byte) {
		if len(msg) > 1<<16 {
			return
		}
		m, err := mdns.ParseDNSMessage(msg)
		if err != nil {
			return
		}
		n := len(m.Questions) + len(m.Answers) + len(m.Authorities) + len(m.Additional)
		if n > len(msg) {
			t.Fatalf("parsed %d questions and records from %d bytes", n, len(msg))
		}
	})
}
