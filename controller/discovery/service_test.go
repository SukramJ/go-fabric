// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func addrStrings(addrs []Address) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestService_AddressesFollowTheSRVTarget(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	s := NewService(n, instQ)
	defer s.Close()
	if s.Reachable() || len(s.Addresses()) != 0 || s.HasTXTRecord() {
		t.Fatal("an unknown service has nothing")
	}
	deliver(t, n, 7,
		[]dns.RR{srvRR(instQ, hostQ, 5540, 120, true), txtRR(instQ, 120, true)},
		aRR(hostQ, "192.0.2.1", 120, true), aaaaRR(hostQ, "2001:db8::1", 120, true),
		aaaaRR(hostQ, "fd00::1", 120, true), aaaaRR(hostQ, "fe80::1", 120, true),
	)
	want := []string{"[fe80::1%eth7]:5540", "[fd00::1]:5540", "[2001:db8::1]:5540", "192.0.2.1:5540"}
	if got := addrStrings(s.Addresses()); !equalStrings(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	if !s.Reachable() || !s.HasTXTRecord() || s.Name().QName() != instQ {
		t.Fatal("a new address marks the service reachable; the TXT is known")
	}
	if len(s.Parameters()) != 0 {
		t.Fatal("an empty TXT has no parameters")
	}
	if u := s.Addresses()[3].UDPAddr(); u.String() != "192.0.2.1:5540" {
		t.Fatalf("UDPAddr = %v", u)
	}
}

func TestService_SeededFromTheCacheAndUnzonedWithoutInterface(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aaaaRR(hostQ, "fe80::2", 120, true))
	s := NewService(n, instQ)
	defer s.Close()
	if got := addrStrings(s.Addresses()); !equalStrings(got, []string{"[fe80::2]:5540"}) {
		t.Fatalf("addresses = %v", got)
	}
}

func TestService_NotificationsCoalesceAndTXTBySignature(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	s := NewService(n, instQ)
	defer s.Close()
	count := 0
	onLoop(t, n, func() { s.onChanged(func() { count++ }) })
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true), txtRR(instQ, 120, true, "D=1")}, aRR(hostQ, "192.0.2.1", 120, true), aRR(hostQ, "192.0.2.2", 120, true))
	onLoop(t, n, func() {
		if count != 1 {
			t.Errorf("notifications = %d, want 1 for one packet", count)
		}
	})
	// The same TXT again (a refresh): same signature, no notification.
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=1")})
	onLoop(t, n, func() {
		if count != 1 {
			t.Errorf("notifications = %d after an unchanged TXT", count)
		}
	})
	// A new TXT that supersedes the old one notifies.
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=2")})
	onLoop(t, n, func() {
		if count != 2 {
			t.Errorf("notifications = %d after a changed TXT", count)
		}
	})
	// The superseded TXT's deletion leaves the parameters as they are.
	clk.advance(n, 2*time.Second)
	if v, _ := s.Parameters().Get("D"); v != "2" {
		t.Fatalf("D = %q", v)
	}
	// Removing the SRV removes its addresses.
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 0, false)})
	clk.advance(n, 2*time.Second)
	if got := s.Addresses(); len(got) != 0 {
		t.Fatalf("addresses after the SRV goodbye = %v", addrStrings(got))
	}
}

func TestService_AddressChanges(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aRR(hostQ, "192.0.2.1", 120, false), aaaaRR(hostQ, "fd00::1", 120, false))
	s := NewService(n, instQ)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := s.AddressChanges(ctx)
	next := func() AddressChange {
		t.Helper()
		select {
		case c := <-ch:
			return c
		case <-time.After(2 * time.Second):
			t.Fatal("no address change")
		}
		return AddressChange{}
	}
	if c := next(); c.Kind != AddressAdded || c.Address.String() != "[fd00::1]:5540" {
		t.Fatalf("first = %+v", c)
	}
	if c := next(); c.Kind != AddressAdded || c.Address.String() != "192.0.2.1:5540" {
		t.Fatalf("second = %+v", c)
	}
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.1", 0, false), aaaaRR(hostQ, "fe80::9", 120, false))
	if c := next(); c.Kind != AddressAdded || c.Address.String() != "[fe80::9]:5540" {
		t.Fatalf("third = %+v", c)
	}
	clk.advance(n, 2*time.Second)
	if c := next(); c.Kind != AddressDeleted || c.Address.String() != "192.0.2.1:5540" {
		t.Fatalf("fourth = %+v", c)
	}
	cancel()
	for range ch { //nolint:revive // drain until closed
	}
}

func TestService_AddressChangesEndWithTheService(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	s := NewService(n, instQ)
	ch := s.AddressChanges(t.Context())
	s.Close()
	s.Close()
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aRR(hostQ, "192.0.2.1", 120, false))
	_ = n.Close()
	for range ch { //nolint:revive // drain until closed
	}
	closed := NewService(n, instQ)
	if closed.Addresses() != nil || closed.Reachable() {
		t.Fatal("a service on a closed cache is empty")
	}
	if _, ok := <-closed.AddressChanges(t.Context()); ok {
		t.Fatal("a closed service streams nothing")
	}
}

func TestDiffAddresses_RestartKeepsInterruptedRoundKnown(t *testing.T) {
	t.Parallel()
	a := Address{IP: mustAddr("fd00::1"), Port: 1}
	b := Address{IP: mustAddr("192.0.2.1"), Port: 1}
	changes, known := diffAddresses(nil, []Address{a, b})
	if len(changes) != 2 || len(known) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	// matter.js counts the whole round as known before delivering it, so
	// a restart after delivering only a does not deliver b again.
	changes, _ = diffAddresses(known, []Address{a, b})
	if len(changes) != 0 {
		t.Fatalf("changes after restart = %+v", changes)
	}
	changes, _ = diffAddresses(known, []Address{b})
	if len(changes) != 1 || changes[0].Kind != AddressDeleted || changes[0].Address.AddrPort() != a.AddrPort() {
		t.Fatalf("delete = %+v", changes)
	}
}
