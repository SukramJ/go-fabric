// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TestRecordsForAddresslessInterfaceAreEmpty: a listed interface the
// advertise policy leaves without addresses carries no service records and
// answers no query — a controller on the same host must never hold an SRV
// without an A or AAAA from a carrier-less container bridge whose multicast
// loops back (connectedhomeip AddressResolve_DefaultImpl.cpp:115-125 at
// 6170af84 times the lookup out; BD-Matter-MdnsFollowsAdvertisedAddresses).
// Regression of the review fix of #437, which answered PTR, SRV and TXT
// there and failed every chip-harness commissioning after AddNOC.
func TestRecordsForAddresslessInterfaceAreEmpty(t *testing.T) {
	t.Parallel()
	const withAddrs, addressless = 1, 2
	r, w, _ := newWiredResponder(map[int][]string{withAddrs: {"fd00::1"}})
	t.Cleanup(func() { _ = r.Close() })
	// Listed (a multicast interface) without advertised addresses.
	w.mu.Lock()
	w.addrsByLink[addressless] = nil
	w.mu.Unlock()
	srv := &dns.SRV{Hdr: dns.RR_Header{Name: "ABCD._matter._tcp.local.", Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120}, Port: 5540, Target: "host.local."}
	setRecords(r, "svc", srv)

	r.mu.Lock()
	if !w.link(addressless).listed {
		r.mu.Unlock()
		t.Fatal("the addressless interface must be listed for the case to hold")
	}
	empty := r.recordsForLocked(addressless, w.link(addressless).addrsFunc())
	full := r.recordsForLocked(withAddrs, w.link(withAddrs).addrsFunc())
	r.mu.Unlock()
	if n := len(empty.byService); n != 0 {
		t.Fatalf("addressless interface carries %d service record sets, want none", n)
	}
	if n := len(full.byService); n != 1 {
		t.Fatalf("interface with addresses carries %d service record sets, want 1", n)
	}

	src := &net.UDPAddr{IP: net.ParseIP("fd00::9"), Port: 5353}
	deliver(t, r, queryMsg([]dns.Question{question("ABCD._matter._tcp.local.", dns.TypeSRV, false)}), addressless, src)
	w.mu.Lock()
	sent := len(w.sent)
	w.mu.Unlock()
	if sent != 0 {
		t.Fatalf("a query on the addressless interface was answered with %d packet(s), want none", sent)
	}
}
