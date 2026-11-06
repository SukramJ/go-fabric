// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

// The two cases matter.js #4808 added to
// packages/node/test/behavior/system/network/ConnectionStateTest.ts at
// db021da0, ported by meaning: a peer with no session, no operational
// address and no address of its operational service reports
// WaitingForDeviceDiscovery, because with nothing to try no establishment
// runs and its unresponsive signal never fires (NetworkClient.ts:390-392,
// :541-546 hasAnyAddress); once discovery finds an address the node is
// Reconnecting (the service's change recomputes the state, :87). matter.js
// blocks the device's DNS-SD and lets the cached records expire; here the
// node is stored without an address and DNS-SD stays silent until the case
// answers it.

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// waitForState waits until n reports want.
func waitForState(t *testing.T, n *Node, want ConnectionState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for n.ConnectionState() != want {
		select {
		case <-ctx.Done():
			t.Fatalf("connection state %v, want %v", n.ConnectionState(), want)
		case <-tick.C:
		}
	}
}

func TestConnectionStateWithoutAddressParity(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	rec := commissionedRecord(0x10)
	rec.Addresses = nil
	h.seed(t, "peer1", rec, settings{})
	r := h.registry(t, nil)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	n := r.Get("peer1")
	if err := n.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	p := n.peer()
	if p == nil || p.Descriptor().OperationalAddress != nil || len(p.Service().Addresses()) != 0 || len(p.Sessions()) != 0 {
		t.Fatal("the peer has an address to try")
	}

	// "reports WaitingForDeviceDiscovery while a peer has no address to try"
	waitForState(t, n, WaitingForDeviceDiscovery)

	// "returns to Reconnecting when discovery finds an address for a peer
	// without a subscription"
	host := "device.local."
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.SRV{Hdr: dns.RR_Header{Name: dns.Fqdn(p.Service().Name().QName()), Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120}, Target: host, Port: 5540},
		&dns.AAAA{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120}, AAAA: []byte{0xfd, 0, 15: 0x10}},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case h.mdns <- mdns.QueryResponse{Msg: b}:
	case <-t.Context().Done():
		t.Fatal("the discovery cache took no response")
	}
	waitForState(t, n, Reconnecting)
	if len(p.Service().Addresses()) == 0 {
		t.Fatal("the service found no address")
	}
}
