// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"

	"thingfabric.dev/go-fabric/mdns"
)

var (
	opCFID = [8]byte{0x5E, 0x11, 0x56, 0x06, 0x03, 0x1B, 0x76, 0x08}
	opNode = uint64(0x1234)
	opQ    = "5E115606031B7608-0000000000001234._matter._tcp.local"
)

// respond answers each query on the fake connection with fn's records.
func respond(t *testing.T, conn *fakeConn, fn func(q mdns.DNSMessage) (answers []dns.RR)) {
	t.Helper()
	conn.mu.Lock()
	conn.onSend = func(msg []byte) {
		q, err := mdns.ParseDNSMessage(msg)
		if err != nil {
			return
		}
		if a := fn(q); len(a) > 0 {
			m := new(dns.Msg)
			m.Response = true
			m.Answer = a
			b, _ := m.Pack()
			conn.out <- mdns.QueryResponse{Msg: b}
		}
	}
	conn.mu.Unlock()
}

func hasQuestion(q mdns.DNSMessage, name string, typ mdns.DNSType) bool {
	for _, qq := range q.Questions {
		if qq.Name == name && qq.Type == typ {
			return true
		}
	}
	return false
}

func TestOperational_ResolveFindsTheSRVTargetsAddress(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	respond(t, conn, func(q mdns.DNSMessage) []dns.RR {
		switch {
		case hasQuestion(q, opQ, TypeSRV) && hasQuestion(q, opQ, TypeTXT):
			return []dns.RR{srvRR(opQ, "ctrl.local", 5540, 120, true), txtRR(opQ, 4500, true, "SII=800")}
		case hasQuestion(q, "ctrl.local", TypeA) && hasQuestion(q, "ctrl.local", TypeAAAA):
			return []dns.RR{aaaaRR("ctrl.local", "fd00::7", 120, true)}
		}
		return nil
	})
	s := n.Service(opCFID, opNode)
	defer s.Close()
	if s.Name().QName() != opQ {
		t.Fatalf("qname = %s", s.Name().QName())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	addrs, err := s.Resolve(ctx)
	if err != nil || len(addrs) != 1 || addrs[0].String() != "[fd00::7]:5540" {
		t.Fatalf("Resolve = %v, %v", addrStrings(addrs), err)
	}
	if sii := s.TXTData().SII; sii == nil || *sii != 800*time.Millisecond {
		t.Fatalf("SII = %v", sii)
	}
	// The resolution's discoveries ended with it.
	onLoop(t, n, func() {
		if len(n.solicitor.discovering) != 0 {
			t.Errorf("discoveries left running: %d", len(n.solicitor.discovering))
		}
	})
}

func TestOperational_ResolveWaitsForANewAddress(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "ctrl.local", 5540, 120, true)}, aRR("ctrl.local", "192.0.2.1", 120, true))
	s := n.Service(opCFID, opNode)
	defer s.Close()
	type res struct {
		addrs []Address
		err   error
	}
	done := make(chan res, 1)
	go func() {
		a, err := s.Resolve(t.Context())
		done <- res{a, err}
	}()
	waitFor(t, func() bool { return asked(t, conn, opQ, TypeSRV) })
	if asked(t, conn, "ctrl.local", TypeA) {
		t.Fatal("a target with an address is not resolved")
	}
	// The known address again does not end it; a new one does.
	deliver(t, n, 0, nil, aRR("ctrl.local", "192.0.2.1", 120, true))
	clk.advance(n, time.Second)
	select {
	case r := <-done:
		t.Fatalf("ended on a known address: %+v", r)
	default:
	}
	deliver(t, n, 0, nil, aaaaRR("ctrl.local", "fe80::1", 120, false))
	r := <-done
	if r.err != nil || len(r.addrs) != 2 {
		t.Fatalf("Resolve = %v, %v", addrStrings(r.addrs), r.err)
	}
}

func TestOperational_ResolveFollowsSRVTargetChanges(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "old.local", 5540, 120, true)}, aRR("old.local", "192.0.2.1", 120, true))
	s := n.Service(opCFID, opNode)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.Resolve(ctx); done <- err }()
	waitFor(t, func() bool { return asked(t, conn, opQ, TypeSRV) })
	// The node moves to a host without a known address. The hosts are
	// re-read when the service notifies — here when the old SRV goes and
	// takes its address along (IpServiceResolution.ts:88-117).
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "new.local", 5540, 120, true)})
	clk.advance(n, time.Second)
	waitFor(t, func() bool { return asked(t, conn, "new.local", TypeAAAA) })
	if asked(t, conn, "old.local", TypeA) {
		t.Fatal("the old host had an address and was never resolved")
	}
	onLoop(t, n, func() {
		if _, ok := n.solicitor.discovering[n.get("new.local")]; !ok {
			t.Error("the new target must be resolving")
		}
	})
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestOperational_ResolveOnAClosedCache(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := n.Service(opCFID, opNode)
	done := make(chan error, 1)
	go func() { _, err := s.Resolve(t.Context()); done <- err }()
	waitFor(t, func() bool { return asked(t, conn, opQ, TypeSRV) })
	_ = n.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Resolve(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

// TestOperational_ResolveBoundsItsHostDiscoveries: one resolution runs at
// most maxHostDiscoveries host discoveries at once, however many SRV
// targets without addresses the service names; a target that waited
// starts when a place frees (review II, U3-ctl-lan-1).
func TestOperational_ResolveBoundsItsHostDiscoveries(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	targets := []string{"t0.local", "t1.local", "t2.local", "t3.local", "t4.local", "t5.local"}
	srvs := make([]dns.RR, 0, 1+len(targets))
	srvs = append(srvs, srvRR(opQ, "known.local", 5540, 120, false))
	for _, tg := range targets {
		srvs = append(srvs, srvRR(opQ, tg, 5540, 120, false))
	}
	deliver(t, n, 0, srvs, aRR("known.local", "192.0.2.1", 120, true))
	s := n.Service(opCFID, opNode)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.Resolve(ctx); done <- err }()
	waitFor(t, func() bool { return asked(t, conn, opQ, TypeSRV) })
	running := func(names ...string) (n0 int) {
		onLoop(t, n, func() {
			for _, nm := range names {
				if _, ok := n.solicitor.discovering[n.get(nm)]; ok {
					n0++
				}
			}
		})
		return n0
	}
	if got := running(targets...); got != maxHostDiscoveries {
		t.Fatalf("%d host discoveries running for %d targets, want %d", got, len(targets), maxHostDiscoveries)
	}
	if got := running("t4.local", "t5.local"); got != 0 {
		t.Fatalf("%d of the last two targets resolving, want both waiting (targets start in SRV order)", got)
	}
	if asked(t, conn, "known.local", TypeA) {
		t.Fatal("a target with an address took a place")
	}
	// The service now names two targets only — the known host's address
	// leaves with its SRV, which re-reads the targets: both resolve.
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{srvRR(opQ, "t4.local", 5540, 120, true), srvRR(opQ, "t5.local", 5540, 120, true)})
	clk.advance(n, time.Second)
	waitFor(t, func() bool { return running("t4.local", "t5.local") == 2 })
	if got := running(targets[:4]...); got != 0 {
		t.Fatalf("%d discoveries left for targets no longer named", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
