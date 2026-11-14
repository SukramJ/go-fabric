// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The bounds on a truncated query and its continuation packets (RFC 6762
// §7.2): an unauthenticated sender on the link controls how many packets
// it sends, what they carry, how long it keeps the TC bit set and from how
// many sources and transaction ids; none of it may grow the responder's
// state without bound or keep a query waiting past its first packet's
// 400–500 ms (review II, R2-P1-net-01).

// heldTruncated reports the truncated queries the responder holds: how
// many, and the most questions and known answers one of them holds.
func heldTruncated(r *Responder) (entries, questions, known int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.answers.truncated {
		questions = max(questions, len(q.msg.Question))
		known = max(known, len(q.msg.Answer))
	}
	return len(r.answers.truncated), questions, known
}

// noiseAnswers are n known answers for records the responder does not own:
// the other instances a querier lists beside the ones it asks about.
func noiseAnswers(n int) []dns.RR {
	out := make([]dns.RR, 0, n)
	for i := range n {
		out = append(out, ptrRR("_matter._tcp.local", "node-"+strconv.Itoa(i)+"._matter._tcp.local"))
	}
	return out
}

func barPTR() dns.RR { return ptrRR("_bar._tcp.local", "bar-1._bar._tcp.local") }

// newBoundFixture is the "foo" fixture on an interface with a /24, and a
// second service "bar" another querier asks about.
func newBoundFixture() (*Responder, *fakeWire, *fakeClock) {
	r, w, clk := newWiredResponder(map[int][]string{1: {"192.168.1.10"}})
	setRecords(r, "foo", fooPTR(), fooSRV(), fooTXT(), fooA())
	setRecords(r, "bar", barPTR())
	return r, w, clk
}

func TestTruncatedQueryBounds(t *testing.T) {
	t.Parallel()
	flooder := &net.UDPAddr{IP: net.ParseIP("192.168.1.99"), Port: 5353}
	other := &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 5353}
	// The foo record set has four records, so a held query keeps at most
	// four known answers: the ones that suppress one of them.
	const ownRecords = 4

	t.Run("a sender that keeps the TC bit set for 1000 packets leaves bounded state, and another querier is answered at once", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		answerOther := func(when string) {
			t.Helper()
			w.take()
			deliver(t, r, queryMsg([]dns.Question{question("_bar._tcp.local", dns.TypePTR, false)}), 1, other)
			got := w.take()
			expectSent(t, got, 1)
			expectResponse(t, got[0], 1, nil, []dns.RR{barPTR()}, nil)
			if t.Failed() {
				t.Fatalf("the other querier %s", when)
			}
		}
		for i := range 1000 {
			// A question of its own in every packet, and twenty known
			// answers for records the responder does not own.
			q := anyQuery(noiseAnswers(20)...)
			q.Question = append(q.Question, question("q"+strconv.Itoa(i)+".example.local", dns.TypeA, false))
			q.Truncated = true
			deliver(t, r, q, 1, flooder)
			entries, questions, known := heldTruncated(r)
			if entries > 1 || questions > maxTruncatedQuestions || known > ownRecords {
				t.Fatalf("after packet %d: %d truncated queries held, the largest with %d questions and %d known answers; want at most 1, %d and %d",
					i+1, entries, questions, known, maxTruncatedQuestions, ownRecords)
			}
			if i == 500 {
				answerOther("during the flood")
			}
			// Faster than the shortest wait: a re-armed wait never runs out.
			clk.Advance(300 * time.Millisecond)
		}
		answerOther("after the flood")
		clk.Advance(time.Second)
		if entries, _, _ := heldTruncated(r); entries != 0 {
			t.Errorf("%d truncated queries held a second after the last packet, want 0", entries)
		}
	})

	t.Run("the wait is counted from the first packet: a continuation that keeps the TC bit set does not extend it", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		first := anyQuery()
		first.Truncated = true
		deliver(t, r, first, 1, flooder)
		clk.Advance(300 * time.Millisecond)
		cont := queryMsg(nil, fooPTR())
		cont.Truncated = true
		deliver(t, r, cont, 1, flooder)
		expectSent(t, w.take(), 0)
		// 500 ms after the first packet the longest wait has run out.
		clk.Advance(200 * time.Millisecond)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})

	t.Run("many spoofed sources and transaction ids leave bounded state; a query past the bound is answered at once", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		for i := range 5000 {
			q := anyQuery()
			q.Id = uint16(i)
			q.Truncated = true
			src := &net.UDPAddr{IP: net.IPv4(192, 168, 1, byte(1+i%250)), Port: 5353}
			deliver(t, r, q, 1, src)
			if entries, _, _ := heldTruncated(r); entries > maxTruncatedQueries {
				t.Fatalf("after query %d: %d truncated queries held, want at most %d", i+1, entries, maxTruncatedQueries)
			}
			if i == maxTruncatedQueries {
				got := w.take()
				expectSent(t, got, 1)
				expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
			}
		}
		clk.Advance(500 * time.Millisecond)
		if entries, _, _ := heldTruncated(r); entries != 0 {
			t.Errorf("%d truncated queries held after their wait, want 0", entries)
		}
	})

	t.Run("a two-packet known-answer continuation suppresses the known answers", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newBoundFixture()
		first := anyQuery(noiseAnswers(30)...)
		first.Truncated = true
		deliver(t, r, first, 1, flooder)
		expectSent(t, w.take(), 0)
		deliver(t, r, queryMsg(nil, append(noiseAnswers(30), fooPTR())...), 1, flooder)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})

	t.Run("a three-packet known-answer continuation suppresses the known answers", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		first := anyQuery(noiseAnswers(30)...)
		first.Truncated = true
		deliver(t, r, first, 1, flooder)
		clk.Advance(10 * time.Millisecond)
		second := queryMsg(nil, append(noiseAnswers(30), fooPTR())...)
		second.Truncated = true
		deliver(t, r, second, 1, flooder)
		clk.Advance(10 * time.Millisecond)
		expectSent(t, w.take(), 0)
		deliver(t, r, queryMsg(nil, fooSRV()), 1, flooder)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooTXT()}, []dns.RR{fooA()})
	})

	t.Run("at the packet bound the query is answered at once with what has arrived", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		first := anyQuery()
		first.Truncated = true
		deliver(t, r, first, 1, flooder)
		for i := 2; i <= maxTruncatedPackets; i++ {
			expectSent(t, w.take(), 0)
			cont := queryMsg(nil, noiseAnswers(5)...)
			if i == 2 {
				cont.Answer = append(cont.Answer, fooSRV())
			}
			cont.Truncated = true
			deliver(t, r, cont, 1, flooder)
		}
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooTXT()}, []dns.RR{fooA()})
		// A packet after the bound starts a query of its own, which has
		// no question and is not answered.
		late := queryMsg(nil, fooPTR())
		late.Truncated = true
		deliver(t, r, late, 1, flooder)
		clk.Advance(500 * time.Millisecond)
		expectSent(t, w.take(), 0)
		if entries, _, _ := heldTruncated(r); entries != 0 {
			t.Errorf("%d truncated queries held after their wait, want 0", entries)
		}
	})

	t.Run("a packet that brings more questions than the bound is answered at once, every question with it", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newBoundFixture()
		q := queryMsg(nil)
		for i := range maxTruncatedQuestions {
			q.Question = append(q.Question, question("q"+strconv.Itoa(i)+".example.local", dns.TypeA, false))
		}
		// The question the responder answers comes last, past the bound.
		q.Question = append(q.Question, question(dummyQName, dns.TypeANY, false))
		q.Truncated = true
		deliver(t, r, q, 1, flooder)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
		if entries, _, _ := heldTruncated(r); entries != 0 {
			t.Errorf("%d truncated queries held, want 0", entries)
		}
	})
}
