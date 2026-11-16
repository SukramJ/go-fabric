// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TestResolutionKeepsOnlyTheTargetsAddresses holds R2-P6-bound-0201: a
// resolve keeps the addresses of its SRV target and of no other host name
// a LAN party multicasts while it runs, and at most
// maxResolvedAddressesPerHost of the target's.
func TestResolutionKeepsOnlyTheTargetsAddresses(t *testing.T) {
	t.Parallel()
	instance := OperationalInstanceQName([8]byte{1}, 1)
	res := newResolution(instance)
	flood := func(p int) {
		m := new(dns.Msg)
		m.Response = true
		for i := range 60 {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: fmt.Sprintf("h%d-%d.local.", p, i), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
				A:   net.IPv4(10, byte(p), byte(i), 1),
			})
		}
		b, err := m.Pack()
		if err != nil {
			t.Fatal(err)
		}
		res.absorb(QueryResponse{Msg: b})
	}
	for p := range 100 {
		flood(p)
	}
	if len(res.hosts) != 0 || len(res.zones) != 0 {
		t.Fatalf("before the SRV: %d host names and %d zones kept, want none", len(res.hosts), len(res.zones))
	}

	// The SRV names the target; the same message carries more of the
	// target's link-local addresses than the bound, without a zone (so
	// none is usable and the resolve would go on), and unrelated names.
	m := new(dns.Msg)
	m.Response = true
	m.Answer = append(m.Answer, &dns.SRV{
		Hdr:    dns.RR_Header{Name: instance, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
		Target: "Target.local.", Port: 5540,
	})
	for i := range 3 * maxResolvedAddressesPerHost {
		m.Extra = append(m.Extra, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: "target.local.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120},
			AAAA: net.ParseIP(fmt.Sprintf("fe80::%x", i+1)),
		}, &dns.A{
			Hdr: dns.RR_Header{Name: fmt.Sprintf("other%d.local.", i), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.IPv4(10, 9, 9, byte(i)),
		})
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	res.absorb(QueryResponse{Msg: b})
	for p := range 10 {
		flood(p)
	}
	if len(res.hosts) != 1 || len(res.hosts["target.local."]) != maxResolvedAddressesPerHost {
		t.Fatalf("after the SRV: hosts %d (target's %d addresses), want the target only with %d", len(res.hosts), len(res.hosts["target.local."]), maxResolvedAddressesPerHost)
	}
}
