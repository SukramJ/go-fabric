// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

// twoInterfaceNames is testNames with a second interface, eth8.
func twoInterfaceNames(t *testing.T) (*Names, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	n := newNames(newFakeConn(), nil, namesConfig{
		clock:         clk,
		random:        func() uint32 { return 0 },
		interfaceName: func(i int) string { return map[int]string{0: "", 7: "eth7", 8: "eth8"}[i] },
	})
	t.Cleanup(func() { _ = n.Close() })
	return n, clk
}

// heardOnTwoInterfaces delivers the instance's SRV and a link-local AAAA
// for its target on eth7, then the same AAAA on eth8: two zoned addresses
// (matter.js parity, IpService.ts:315-328).
func heardOnTwoInterfaces(t *testing.T, n *Names, s *Service) {
	t.Helper()
	deliver(t, n, 7, []dns.RR{srvRR(instQ, hostQ, 5540, 120, false)}, aaaaRR(hostQ, "fe80::1", 120, false))
	deliver(t, n, 8, nil, aaaaRR(hostQ, "fe80::1", 120, false))
	want := []string{"[fe80::1%eth7]:5540", "[fe80::1%eth8]:5540"}
	if got := addrStrings(s.Addresses()); !equalStrings(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
}

// TestService_LinkLocalLeavesOnEveryInterface: a link-local address heard
// on two interfaces leaves the service with its SRV target, and with its
// own AAAA's expiry, on both (BD-Matter-LinkLocalZoneVariants; matter.js
// removes only the variant of the record's last interface,
// IpService.ts:330-340); another target's variant of the same IP stays.
func TestService_LinkLocalLeavesOnEveryInterface(t *testing.T) {
	t.Parallel()
	t.Run("SRV deleted", func(t *testing.T) {
		t.Parallel()
		n, clk := twoInterfaceNames(t)
		s := NewService(n, instQ)
		defer s.Close()
		heardOnTwoInterfaces(t, n, s)
		deliver(t, n, 8, []dns.RR{srvRR(instQ, hostQ, 5540, 0, false)})
		clk.advance(n, 2*time.Second)
		if got := addrStrings(s.Addresses()); len(got) != 0 {
			t.Fatalf("addresses after the SRV left = %v, want none", got)
		}
	})
	t.Run("another target's variant stays", func(t *testing.T) {
		t.Parallel()
		n, clk := twoInterfaceNames(t)
		s := NewService(n, instQ)
		defer s.Close()
		deliver(t, n, 7, []dns.RR{srvRR(instQ, hostQ, 5540, 120, false)}, aaaaRR(hostQ, "fe80::1", 120, false))
		deliver(t, n, 8, []dns.RR{srvRR(instQ, "other.local", 5540, 120, false)}, aaaaRR("other.local", "fe80::1", 120, false))
		if got := addrStrings(s.Addresses()); !equalStrings(got, []string{"[fe80::1%eth7]:5540", "[fe80::1%eth8]:5540"}) {
			t.Fatalf("addresses = %v", got)
		}
		deliver(t, n, 7, []dns.RR{srvRR(instQ, hostQ, 5540, 0, false)})
		clk.advance(n, 2*time.Second)
		got := s.Addresses()
		if len(got) != 1 || got[0].String() != "[fe80::1%eth8]:5540" || got[0].Host != "other.local" {
			t.Fatalf("addresses after the first target left = %v, want other.local's [fe80::1%%eth8]:5540", got)
		}
	})
	t.Run("AAAA expired", func(t *testing.T) {
		t.Parallel()
		n, clk := twoInterfaceNames(t)
		s := NewService(n, instQ)
		defer s.Close()
		heardOnTwoInterfaces(t, n, s)
		deliver(t, n, 8, []dns.RR{srvRR(instQ, hostQ, 5540, 4500, false)})
		clk.advance(n, 10*time.Minute)
		if got := addrStrings(s.Addresses()); len(got) != 0 {
			t.Fatalf("addresses after the AAAA expired = %v, want none", got)
		}
	})
}
