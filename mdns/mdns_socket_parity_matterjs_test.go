// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestMdnsSocketQuerySplittingParityMatterJS mirrors
// packages/general/test/net/dns-sd/MdnsSocketTest.ts:229-460 at 862ad75f
// (the query halves of "message splitting" and "query splitting"): a
// query that fits is one message; questions that do not fit are split,
// every message a query within MaxMessageSize and without known answers;
// a query whose known answers do not fit carries its question in the first
// message only.
func TestMdnsSocketQuerySplittingParityMatterJS(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, msgs [][]byte) []mdns.DNSMessage {
		t.Helper()
		out := make([]mdns.DNSMessage, 0, len(msgs))
		for _, b := range msgs {
			if len(b) > mdns.MaxMessageSize {
				t.Errorf("a message of %d bytes, over %d", len(b), mdns.MaxMessageSize)
			}
			m, err := mdns.ParseDNSMessage(b)
			if err != nil {
				t.Fatal(err)
			}
			if m.Response {
				t.Error("a split query message is not a query")
			}
			out = append(out, m)
		}
		return out
	}
	questions := func(n int) []mdns.DNSQuestion {
		qs := make([]mdns.DNSQuestion, 0, n)
		for i := range n {
			qs = append(qs, mdns.DNSQuestion{Name: fmt.Sprintf("service%03dxxxxxxxxxxxxxxxxxxxx._matter._tcp.local", i), Type: mdns.DNSType(255)})
		}
		return qs
	}
	total := func(ms []mdns.DNSMessage) (q, a int) {
		for _, m := range ms {
			q += len(m.Questions)
			a += len(m.Answers)
		}
		return q, a
	}
	t.Run("clears queries after first split message", func(t *testing.T) {
		t.Parallel()
		known := make([]mdns.DNSRecord, 0, 40)
		for i := range 40 {
			known = append(known, mdns.DNSRecord{
				Name: "_matter._tcp.local", Type: mdns.DNSTypePTR, TTL: 120,
				PTR: fmt.Sprintf("device%03d-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx._matter._tcp.local", i),
			})
		}
		ms := parse(t, mustPack(t, []mdns.DNSQuestion{{Name: "_matter._tcp.local", Type: mdns.DNSTypePTR}}, known))
		if len(ms) < 2 || len(ms[0].Questions) != 1 {
			t.Fatalf("%d messages, the first with %d questions; want a split with the question first", len(ms), len(ms[0].Questions))
		}
		for _, m := range ms[1:] {
			if len(m.Questions) != 0 {
				t.Errorf("a follow-up message carries %d questions", len(m.Questions))
			}
		}
		if _, a := total(ms); a != 40 {
			t.Errorf("%d known answers sent, want 40", a)
		}
	})
	t.Run("sends queries in single message when they fit (1 packet)", func(t *testing.T) {
		t.Parallel()
		var qs []mdns.DNSQuestion
		for i := 1; i <= 5; i++ {
			qs = append(qs, mdns.DNSQuestion{Name: fmt.Sprintf("_svc%d._tcp.local", i), Type: mdns.DNSType(255)})
		}
		ms := parse(t, mustPack(t, qs, nil))
		if len(ms) != 1 || len(ms[0].Questions) != 5 {
			t.Fatalf("%d messages; want one with five questions", len(ms))
		}
	})
	for _, c := range []struct {
		it      string
		n, msgs int
	}{
		{"splits queries into 2 packets when they exceed message size", 25, 2},
		{"splits queries into 3 packets when they are very large", 50, 3},
	} {
		t.Run(c.it, func(t *testing.T) {
			t.Parallel()
			ms := parse(t, mustPack(t, questions(c.n), nil))
			if q, _ := total(ms); len(ms) != c.msgs || q != c.n {
				t.Fatalf("%d messages with %d questions, want %d with %d", len(ms), q, c.msgs, c.n)
			}
		})
	}
	t.Run("does not include known-answers when splitting queries", func(t *testing.T) {
		t.Parallel()
		known := []mdns.DNSRecord{{Name: "_matter._tcp.local", Type: mdns.DNSTypePTR, TTL: 120, PTR: "device._matter._tcp.local"}}
		ms := parse(t, mustPack(t, questions(25), known))
		if _, a := total(ms); len(ms) < 2 || a != 0 {
			t.Fatalf("%d messages with %d known answers; want a split without any", len(ms), a)
		}
	})
	t.Run("preserves message type when splitting queries", func(t *testing.T) {
		t.Parallel()
		if ms := parse(t, mustPack(t, questions(25), nil)); len(ms) < 2 {
			t.Fatalf("%d messages, want a split", len(ms))
		}
	})
}

func mustPack(t *testing.T, qs []mdns.DNSQuestion, known []mdns.DNSRecord) [][]byte {
	t.Helper()
	msgs, err := mdns.PackDNSQueries(qs, known)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}
