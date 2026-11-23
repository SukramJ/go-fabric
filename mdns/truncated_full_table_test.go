// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// longQuestionName is a name of 254 characters (four labels), unique per
// (query, question): the largest question a held query keeps.
func longQuestionName(query, q int) string {
	label := func(c byte, n int) string { return strings.Repeat(string(c), n) }
	head := strconv.Itoa(query) + "-" + strconv.Itoa(q) + "-"
	return head + label('a', 63-len(head)) + "." + label('b', 63) + "." + label('c', 63) + "." + label('d', 61) + "."
}

// heldWireBytes is the packed size of every truncated query the responder
// holds, summed: the state the held queries keep, measured as the bytes
// their messages take on the wire.
func heldWireBytes(t *testing.T, r *Responder) int {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, q := range r.answers.truncated {
		total += q.msg.Len()
	}
	return total
}

// TestTruncatedQueryBesideAFullTable measures a querier whose known-answer
// list spans two packets (RFC 6762 §7.2) while [maxTruncatedQueries] other
// truncated queries are held, each with [maxTruncatedQuestions] of the
// longest questions in packets no larger than the socket's read buffer:
// whether the querier is still answered, when, with what, and what the
// held queries amount to. The same querier without the other queries is
// the reference.
func TestTruncatedQueryBesideAFullTable(t *testing.T) {
	t.Parallel()
	querier := &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 5353}
	// The socket reads at most 9000 bytes of one datagram (socket.go).
	const readBuffer = 9000
	firstPacket := func() *dns.Msg {
		q := anyQuery(noiseAnswers(30)...)
		q.Id = 0x4242
		q.Truncated = true
		return q
	}
	continuation := func() *dns.Msg {
		q := queryMsg(nil, append(noiseAnswers(30), fooPTR(), fooSRV())...)
		q.Id = 0x4242
		return q
	}

	t.Run("reference: the held query is answered at its continuation, without the records it listed", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		deliver(t, r, firstPacket(), 1, querier)
		expectSent(t, w.take(), 0)
		clk.Advance(50 * time.Millisecond)
		deliver(t, r, continuation(), 1, querier)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooTXT()}, []dns.RR{fooA()})
		t.Logf("reference: answered 50 ms after the first packet, %d bytes", got[0].msg.Len())
	})

	t.Run("beside a full table the query is answered at its first packet, the second packet's known answers unused", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newBoundFixture()
		packets, packetBytes := 0, 0
		send := func(m *dns.Msg, src *net.UDPAddr) {
			t.Helper()
			if n := m.Len(); n > readBuffer {
				t.Fatalf("a packet of %d bytes, the socket reads at most %d", n, readBuffer)
			}
			packets++
			packetBytes += m.Len()
			deliver(t, r, m, 1, src)
		}
		// Each held query: two truncated packets that together carry
		// maxTruncatedQuestions distinct questions, none the responder
		// answers.
		const firstHalf = 33
		for i := range maxTruncatedQueries {
			src := &net.UDPAddr{IP: net.IPv4(192, 168, 1, byte(100+i)), Port: 5353}
			for _, part := range [][2]int{{0, firstHalf}, {firstHalf, maxTruncatedQuestions}} {
				m := queryMsg(nil)
				m.Id = uint16(i) //nolint:gosec // G115: i < 64
				m.Truncated = true
				for q := part[0]; q < part[1]; q++ {
					m.Question = append(m.Question, question(longQuestionName(i, q), dns.TypeA, false))
				}
				send(m, src)
			}
		}
		expectSent(t, w.take(), 0)
		entries, questions, _ := heldTruncated(r)
		held := heldWireBytes(t, r)
		if entries != maxTruncatedQueries || questions != maxTruncatedQuestions {
			t.Fatalf("%d queries held, the largest with %d questions; want %d and %d", entries, questions, maxTruncatedQueries, maxTruncatedQuestions)
		}
		t.Logf("table full after %d packets (%d bytes): %d queries held, %d questions each, %d bytes packed",
			packets, packetBytes, entries, questions, held)

		// The querier's first packet finds no room: answered at once,
		// with every record, since the known answers that suppress them
		// are still to come.
		deliver(t, r, firstPacket(), 1, querier)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
		t.Logf("beside the full table: answered at the first packet, %d bytes", got[0].msg.Len())
		// Its continuation carries no question and is not held (the
		// table is still full): nothing more is sent.
		clk.Advance(50 * time.Millisecond)
		deliver(t, r, continuation(), 1, querier)
		expectSent(t, w.take(), 0)
		if e, _, _ := heldTruncated(r); e > maxTruncatedQueries {
			t.Fatalf("%d queries held, want at most %d", e, maxTruncatedQueries)
		}

		// The held queries end with their wait: nothing is answered for
		// them (no question names a record of the responder) and nothing
		// stays.
		clk.Advance(500 * time.Millisecond)
		expectSent(t, w.take(), 0)
		if e, _, _ := heldTruncated(r); e != 0 {
			t.Fatalf("%d queries held after their wait, want 0", e)
		}
	})
}
