// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

func questionsOf(m mdns.DNSMessage) []string {
	out := make([]string, 0, len(m.Questions))
	for _, q := range m.Questions {
		out = append(out, q.Name+"/"+q.Type.String())
	}
	return out
}

func TestSolicitor_SolicitationsOfOneTaskShareAPacket(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := n.Solicitor()
	onLoop(t, n, func() {
		a, b := n.get(instQ), n.get(hostQ)
		s.solicit(&solicitation{name: a, types: []mdns.DNSType{TypeSRV}})
		s.solicit(&solicitation{name: a, types: []mdns.DNSType{TypeTXT, TypeSRV}})
		s.solicit(&solicitation{name: b, types: []mdns.DNSType{TypeAAAA}})
	})
	qs := conn.queries(t)
	if len(qs) != 1 {
		t.Fatalf("packets = %d, want 1", len(qs))
	}
	want := []string{instQ + "/SRV", instQ + "/TXT", hostQ + "/AAAA"}
	if got := questionsOf(qs[0]); !equalStrings(got, want) {
		t.Fatalf("questions = %v, want %v", got, want)
	}
	// An empty solicitation sends nothing.
	conn.reset()
	s.Solicit(Solicitation{Name: n.Get(instQ)})
	if len(conn.queries(t)) != 0 {
		t.Fatal("no record types, no packet")
	}
}

func TestSolicitor_KnownAnswersIncludeAssociatedNames(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aRR(hostQ, "192.0.2.1", 120, true))
	host := n.Get(hostQ)
	n.Solicitor().Solicit(Solicitation{
		Name:            n.Get(instQ),
		RecordTypes:     []mdns.DNSType{TypeSRV},
		AssociatedNames: func() []*Name { return []*Name{host} },
	})
	qs := conn.queries(t)
	if len(qs) != 1 || len(qs[0].Answers) != 2 {
		t.Fatalf("queries = %+v, want one with two known answers", qs)
	}
	for _, a := range qs[0].Answers {
		if a.CacheFlush {
			t.Errorf("known answer %v carries the cache-flush bit", a.Type)
		}
	}
	if qs[0].Answers[0].Type != TypeSRV || qs[0].Answers[0].TTL != 120 || qs[0].Answers[1].IP.String() != "192.0.2.1" {
		t.Fatalf("known answers = %+v", qs[0].Answers)
	}
}

func TestSolicitor_DiscoverRunsOneScheduleForAllCallers(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := n.Solicitor()
	name := n.Get(instQ)
	d := Discovery{Solicitation: Solicitation{Name: name, RecordTypes: []mdns.DNSType{TypeSRV}}}
	ctx1, cancel1 := context.WithCancel(t.Context())
	ctx2, cancel2 := context.WithCancel(t.Context())
	done1, done2 := make(chan error, 1), make(chan error, 1)
	go func() { done1 <- s.Discover(ctx1, d) }()
	waitFor(t, func() bool { return len(conn.queries(t)) == 1 })
	go func() { done2 <- s.Discover(ctx2, d) }()
	waitFor(t, func() bool {
		joined := false
		onLoop(t, n, func() { joined = len(s.discovering[name].waiting) == 2 })
		return joined
	})
	if got := len(conn.queries(t)); got != 1 {
		t.Fatalf("queries after the second caller joined = %d, want 1", got)
	}
	clk.advance(n, time.Second)
	if got := len(conn.queries(t)); got != 2 {
		t.Fatalf("queries after 1 s = %d, want 2", got)
	}
	cancel1()
	if err := <-done1; err != nil {
		t.Fatal(err)
	}
	clk.advance(n, 2*time.Second)
	if got := len(conn.queries(t)); got != 3 {
		t.Fatalf("queries after 3 s = %d, want 3 (the second caller keeps it alive)", got)
	}
	cancel2()
	if err := <-done2; err != nil {
		t.Fatal(err)
	}
	clk.advance(n, time.Hour)
	if got := len(conn.queries(t)); got != 3 {
		t.Fatalf("queries after both left = %d, want 3", got)
	}
}

func TestSolicitor_DiscoverEndsWithTheScheduleAndTheCache(t *testing.T) {
	t.Parallel()
	n, conn, clk := testNames(t)
	s := n.Solicitor()
	done := make(chan error, 1)
	go func() {
		done <- s.Discover(t.Context(), Discovery{
			Solicitation: Solicitation{Name: n.Get(instQ), RecordTypes: []mdns.DNSType{TypeTXT}},
			Retries:      &Retries{MaximumCount: 2},
		})
	}()
	waitFor(t, func() bool { return len(conn.queries(t)) == 1 })
	clk.advance(n, 4*time.Second)
	if err := <-done; err != nil {
		t.Fatalf("exhausted schedule: %v", err)
	}
	if got := len(conn.queries(t)); got != 3 {
		t.Fatalf("queries = %d, want 3 (one now, two retries)", got)
	}

	go func() {
		done <- s.Discover(t.Context(), Discovery{Solicitation: Solicitation{Name: n.Get(hostQ), RecordTypes: []mdns.DNSType{TypeA}}})
	}()
	waitFor(t, func() bool { return len(conn.queries(t)) == 4 })
	_ = n.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("closed cache: %v", err)
	}
	if err := s.Discover(t.Context(), Discovery{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("discover after close: %v", err)
	}
	onLoopClosed := n.loop.call(func() {})
	if !errors.Is(onLoopClosed, ErrClosed) {
		t.Fatal("loop must be stopped")
	}
}

func TestSolicitor_ClosedSolicitorSendsNothing(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	onLoop(t, n, func() {
		n.solicitor.close()
		n.solicitor.solicit(&solicitation{name: n.get(instQ), types: []mdns.DNSType{TypeSRV}})
		n.solicitor.scheduled = true
		n.solicitor.emit()
	})
	if len(conn.queries(t)) != 0 {
		t.Fatal("a closed solicitor must not send")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
