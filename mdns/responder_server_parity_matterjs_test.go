// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// A port of matter.js packages/protocol/test/mdns/MdnsServerTest.ts at
// 862ad75f against the responder's answer engine: the same record
// fixtures, the same queries, the same responses, the same unicast,
// suppression and splitting decisions. The "Host name" group runs against
// the module's own host-name choice (hostNameChooser), the rule
// notes/parity/by_design.md L7-D03 keeps.

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	dummyQName = "a.b.c.d."
	dummyIP    = "1.2.3.4"
)

// dummySource is the querier, DUMMY_IP on INTERFACE_NAME (:37-39).
var dummySource = &net.UDPAddr{IP: net.ParseIP(dummyIP), Port: 5353}

// The "foo" generator of the beforeEach (:73-78).
func fooPTR() dns.RR { return ptrRR(dummyQName, "abcd") }
func fooSRV() dns.RR { return srvRec(dummyQName, 1234, "abcd.local") }
func fooTXT() dns.RR { return txtRR(dummyQName, "A=1", "B=2") }
func fooA() dns.RR   { return addrRR("abcd.local", dummyIP) }

// newServerFixture is the beforeEach: a responder on interface 1 with the
// "foo" records.
func newServerFixture() (*Responder, *fakeWire, *fakeClock) {
	r, w, clk := newWiredResponder(map[int][]string{1: {dummyIP}})
	setRecords(r, "foo", fooPTR(), fooSRV(), fooTXT(), fooA())
	return r, w, clk
}

func anyQuery(known ...dns.RR) *dns.Msg {
	return queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, false)}, known...)
}

func TestMdnsServerRespondsToStandardQueries(t *testing.T) {
	t.Parallel()
	t.Run("server responds to an ANY query with different Query messageTypes (Query)", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
		if got[0].msg.Id != 0 {
			t.Errorf("transaction id %d, want 0", got[0].msg.Id)
		}
	})
	t.Run("server responds to an ANY query with different Query messageTypes (Truncated Query)", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		q := anyQuery()
		q.Truncated = true
		deliver(t, r, q, 1, dummySource)
		expectSent(t, w.take(), 0) // Not yet there
		clk.Advance(500 * time.Millisecond)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})
	for _, c := range []struct {
		name   string
		qname  string
		qtype  uint16
		answer dns.RR
	}{
		{"server responds to an PTR query", dummyQName, dns.TypePTR, fooPTR()},
		{"server responds to an SRV query", dummyQName, dns.TypeSRV, fooSRV()},
		{"server responds to an A query with A record only", "abcd.local", dns.TypeA, fooA()},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, w, _ := newServerFixture()
			deliver(t, r, queryMsg([]dns.Question{question(c.qname, c.qtype, false)}), 1, dummySource)
			got := w.take()
			expectSent(t, got, 1)
			// A PTR or SRV question gets the rest of the set except the
			// PTRs as additionals; an A question none (MdnsServer.ts:130-136).
			var additional []dns.RR
			switch c.qtype {
			case dns.TypePTR:
				additional = []dns.RR{fooSRV(), fooTXT(), fooA()}
			case dns.TypeSRV:
				additional = []dns.RR{fooTXT(), fooA()}
			}
			expectResponse(t, got[0], 1, nil, []dns.RR{c.answer}, additional)
		})
	}
}

func TestMdnsServerCacheFlushBit(t *testing.T) {
	t.Parallel()
	answers := func(ps []sentPacket) []dns.RR {
		var out []dns.RR
		for _, p := range ps {
			out = append(out, p.msg.Answer...)
		}
		return out
	}
	t.Run("suppresses a record the querier knows even though we flag it cache-flush", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		setRecords(r, "foo", fooPTR(), flushed(fooSRV()))
		deliver(t, r, anyQuery(fooSRV()), 1, dummySource)
		expectRRs(t, "answers", answers(w.take()), fooPTR())
	})
	t.Run("suppresses only while more than half the known answer's lifetime remains", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		for _, c := range []struct {
			remaining  uint32
			suppressed bool
		}{{61, true}, {60, false}, {59, false}} {
			setRecords(r, "foo", fooPTR(), flushed(fooSRV()))
			deliver(t, r, anyQuery(ttl(fooSRV(), c.remaining)), 1, dummySource)
			sentSRV := false
			for _, rr := range answers(w.take()) {
				sentSRV = sentSRV || rr.Header().Rrtype == dns.TypeSRV
			}
			if sentSRV == c.suppressed {
				t.Errorf("known answer with %d s left: SRV sent=%v, want %v", c.remaining, sentSRV, !c.suppressed)
			}
			clk.Advance(130 * time.Second)
		}
	})
	t.Run("drops the bit on an additional record whose set a known answer pruned", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		first, second := addrRR("abcd.local", dummyIP), addrRR("abcd.local", "5.6.7.8")
		setRecords(r, "foo", fooPTR(), flushed(first), flushed(second))
		deliver(t, r, anyQuery(first), 1, dummySource)
		var extra []dns.RR
		for _, p := range w.take() {
			extra = append(extra, p.msg.Extra...)
		}
		expectRRs(t, "additionals", extra, second)
	})
	t.Run("omits the bit from a unicast response", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		setRecords(r, "foo", fooPTR(), flushed(fooSRV()))
		// A unicast request is answered by multicast unless the records
		// went out as multicast recently, so establish that first.
		deliver(t, r, anyQuery(), 1, dummySource)
		clk.Advance(time.Second)
		w.take()
		deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, true)}), 1, dummySource)
		got := w.take()
		sent := answers(got)
		if len(sent) == 0 || got[0].dst == nil {
			t.Fatalf("want a unicast answer, got %d packets", len(got))
		}
		for _, rr := range sent {
			if rr.Header().Class&classCacheFlush != 0 {
				t.Errorf("unicast answer %v carries the cache-flush bit", rr)
			}
		}
	})
	t.Run("drops the bit when a known answer leaves the record set incomplete", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		first, second := addrRR("abcd.local", dummyIP), addrRR("abcd.local", "5.6.7.8")
		setRecords(r, "foo", flushed(first), flushed(second))
		deliver(t, r, queryMsg([]dns.Question{question("abcd.local", dns.TypeA, false)}, first), 1, dummySource)
		expectRRs(t, "answers", answers(w.take()), second)
	})
	t.Run("keeps the bit when the whole record set is sent", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		first, second := addrRR("abcd.local", dummyIP), addrRR("abcd.local", "5.6.7.8")
		setRecords(r, "foo", flushed(first), flushed(second))
		deliver(t, r, queryMsg([]dns.Question{question("abcd.local", dns.TypeA, false)}), 1, dummySource)
		expectRRs(t, "answers", answers(w.take()), flushed(first), flushed(second))
	})
}

func TestMdnsServerAnswerSuppression(t *testing.T) {
	t.Parallel()
	t.Run("suppress one answers in an ANY query", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(fooPTR()), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})
	t.Run("suppress some answers in an ANY query", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(fooTXT(), fooPTR()), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV()}, []dns.RR{fooA()})
	})
	t.Run("suppress full answer in an ANY query", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(fooPTR(), fooSRV(), fooTXT()), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
	t.Run("suppress full answer in an A query", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{question("abcd.local", dns.TypeA, false)}, fooA()), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
}

func TestMdnsServerHandleDuplicateMessages(t *testing.T) {
	t.Parallel()
	full := func(t *testing.T, got []sentPacket) {
		t.Helper()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
	}
	t.Run("server suppress response to same ANY query when 0ms delay", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(), 1, dummySource)
		full(t, w.take())
		deliver(t, r, anyQuery(), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
	t.Run("server do not suppress response from same ANY query when > 1s delay", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		deliver(t, r, anyQuery(), 1, dummySource)
		full(t, w.take())
		clk.Advance(questionSuppressionWindow + 2*time.Millisecond)
		deliver(t, r, anyQuery(), 1, dummySource)
		full(t, w.take())
	})
	t.Run("server also suppress response to different query with same records when 0ms delay", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(), 1, dummySource)
		full(t, w.take())
		deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeA, false)}), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
	t.Run("server do not suppress additional records to different query when 0ms delay", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{question("abcd.local", dns.TypeA, false)}), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooA()}, nil)
		deliver(t, r, anyQuery(), 1, dummySource)
		full(t, w.take())
	})
}

func TestMdnsServerRespondsAsUnicast(t *testing.T) {
	t.Parallel()
	// QUERY and RESPONSE (:935-961): an ANY question with the QU bit.
	qu := func() *dns.Msg { return queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, true)}) }
	qm := func() *dns.Msg { return queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, false)}) }
	expect := func(t *testing.T, got []sentPacket, unicast bool) {
		t.Helper()
		expectSent(t, got, 1)
		var dst *net.UDPAddr
		if unicast {
			dst = dummySource
		}
		expectResponse(t, got[0], 1, dst, []dns.RR{fooPTR(), fooSRV(), fooTXT()}, []dns.RR{fooA()})
	}
	t.Run("server responds to an ANY query as multicast if requested via unicast but records were never sent before", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), false)
	})
	t.Run("server responds to an ANY query as multicast when unicast is requested after a previous unicast query with less than 1/4 of TTL remaining", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), false)
		clk.Advance(29 * time.Second) // less than 1/4 of ttl
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), true)
	})
	t.Run("server responds to an ANY query as multicast even if requested as unicast and after a multicast query with more than 1/4 of ttl but never sent as multicast", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), false)
		clk.Advance(31 * time.Second) // more than 1/4 of ttl
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), false)
	})
	t.Run("server responds to an ANY query as multicast even if requested as unicast and after a multicast query with more than 1/4 of ttl when earlier sent as multicast", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		deliver(t, r, qm(), 1, dummySource)
		expect(t, w.take(), false)
		clk.Advance(20 * time.Second)
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), true)
		clk.Advance(11 * time.Second) // more than 1/4 of ttl since the multicast
		deliver(t, r, qu(), 1, dummySource)
		expect(t, w.take(), false)
	})
}

// twelve or sixteen copies of the "foo" quadruple (:1059-1075,
// :1135-1151).
func quadruples(n int) (records, answers, additional []dns.RR) {
	for range n {
		records = append(records, fooPTR(), fooSRV(), fooTXT(), fooA())
	}
	for i := 0; i < len(records); i += 4 {
		answers = append(answers, records[i], records[i+1], records[i+2])
		additional = append(additional, records[i+3])
	}
	return records, answers, additional
}

func TestMdnsServerSplitsResponse(t *testing.T) {
	t.Parallel()
	t.Run("include as many answers as possible and spill remaining additionalRecords into a follow-up packet", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		records, answers, additional := quadruples(12)
		setRecords(r, "foo", records...)
		deliver(t, r, anyQuery(), 1, dummySource)
		got := w.take()
		expectSent(t, got, 2)
		expectResponse(t, got[0], 1, nil, answers, additional[:5])
		expectResponse(t, got[1], 1, nil, nil, additional[5:])
	})
	t.Run("split answers into multiple responses and add additional records in last one", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		records, answers, additional := quadruples(16)
		setRecords(r, "foo", records...)
		deliver(t, r, anyQuery(), 1, dummySource)
		got := w.take()
		expectSent(t, got, 2)
		// No truncation bit: it is not relevant for responses.
		expectResponse(t, got[0], 1, nil, answers[:len(answers)-7], nil)
		expectResponse(t, got[1], 1, nil, answers[len(answers)-7:], additional)
	})
}

func TestMdnsServerDuplicateQuestionSuppression(t *testing.T) {
	t.Parallel()
	ptrQuery := func() *dns.Msg { return queryMsg([]dns.Question{question(dummyQName, dns.TypePTR, false)}) }
	t.Run("suppresses response when same question was recently answered", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, ptrQuery(), 1, dummySource)
		expectSent(t, w.take(), 1)
		deliver(t, r, ptrQuery(), 1, dummySource)
		expectSent(t, w.take(), 0) // Suppressed
	})
	t.Run("responds after suppression window expires", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newServerFixture()
		deliver(t, r, ptrQuery(), 1, dummySource)
		expectSent(t, w.take(), 1)
		clk.Advance(questionSuppressionWindow + 2*time.Millisecond)
		deliver(t, r, ptrQuery(), 1, dummySource)
		expectSent(t, w.take(), 1) // Should respond after window expires
	})
}

func TestMdnsServerUnknownNameQueryRejection(t *testing.T) {
	t.Parallel()
	t.Run("does not respond to queries for names we do not serve", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{
			question("_googlecast._tcp.local", dns.TypePTR, false),
			question("_airplay._tcp.local", dns.TypeANY, false),
		}), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
	t.Run("responds to A-record host name even though it differs from the service qname", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{question("abcd.local", dns.TypeA, false)}), 1, dummySource)
		expectSent(t, w.take(), 1)
	})
	t.Run("still responds when a known-name query is mixed with unknown-name queries", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{
			question("_googlecast._tcp.local", dns.TypePTR, false),
			question(dummyQName, dns.TypeANY, false),
		}), 1, dummySource)
		expectSent(t, w.take(), 1)
	})
	t.Run("matches uppercase query names against lowercase records (RFC 6762 §16)", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, queryMsg([]dns.Question{question("A.B.C.D.", dns.TypeANY, false)}), 1, dummySource)
		expectSent(t, w.take(), 1)
	})
	t.Run("lets zero-query continuation packets merge known-answers into a cached TC fragment", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		first := anyQuery()
		first.Truncated = true
		deliver(t, r, first, 1, dummySource)
		expectSent(t, w.take(), 0)
		deliver(t, r, queryMsg(nil, fooPTR()), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})
	t.Run("picks up new names after the records generator is replaced", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		newQName := "other.service.local."
		setRecords(r, "foo", ptrRR(newQName, "efgh"), srvRec(newQName, 5678, "efgh.local"))
		deliver(t, r, queryMsg([]dns.Question{question(newQName, dns.TypeANY, false)}), 1, dummySource)
		expectSent(t, w.take(), 1)
		deliver(t, r, anyQuery(), 1, dummySource)
		expectSent(t, w.take(), 0)
	})
	t.Run("merges known-answers from a TC continuation that carries unrelated queries", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		first := anyQuery()
		first.Truncated = true
		deliver(t, r, first, 1, dummySource)
		expectSent(t, w.take(), 0)
		deliver(t, r, queryMsg([]dns.Question{question("_googlecast._tcp.local", dns.TypePTR, false)}, fooPTR()), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})
	t.Run("suppresses known-answer records supplied with a different case (RFC 6762 §16)", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		deliver(t, r, anyQuery(ptrRR("A.B.C.D.", "abcd")), 1, dummySource)
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, []dns.RR{fooSRV(), fooTXT()}, []dns.RR{fooA()})
	})
}

// levelRecorder is an slog handler that keeps the level of every record.
type levelRecorder struct{ levels *[]slog.Level }

func (h levelRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h levelRecorder) Handle(_ context.Context, r slog.Record) error {
	*h.levels = append(*h.levels, r.Level)
	return nil
}
func (h levelRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h levelRecorder) WithGroup(string) slog.Handler      { return h }

// The "Close" group (:1624-1668): after Close a broadcast sends nothing,
// and the goodbyes Close sends log nothing above debug.
func TestMdnsServerClose(t *testing.T) {
	t.Parallel()
	expectQuiet := func(t *testing.T, levels []slog.Level) {
		t.Helper()
		for _, l := range levels {
			if l > slog.LevelDebug {
				t.Errorf("logged at %v while closing", l)
			}
		}
	}
	t.Run("sends nothing and logs at debug level for a broadcast after close", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		var levels []slog.Level
		r.SetLogger(slog.New(levelRecorder{&levels}))
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		w.take()
		r.TriggerReannounce(context.Background())
		r.ServiceDisconnected("foo", "")
		expectSent(t, w.take(), 0)
		expectQuiet(t, levels)
	})
	t.Run("logs at debug level when expiring announcements while the server closes", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newServerFixture()
		var levels []slog.Level
		r.SetLogger(slog.New(levelRecorder{&levels}))
		r.TriggerReannounce(context.Background())
		w.take()
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		// The goodbye of "foo": its records with TTL 0, the cache-flush
		// bit on the unique ones whose set is left empty
		// (InterfaceResponder.ts:503-507 at db021da0).
		got := w.take()
		expectSent(t, got, 1)
		expectResponse(t, got[0], 1, nil, goodbyeOf([]dns.RR{fooPTR(), fooSRV(), fooTXT(), fooA()}), nil)
		expectQuiet(t, levels)
	})
}

// The "Host name" group (MdnsServerTest.ts:1670-1836) runs against the
// host-name choice of [defaultHostName]: one name, the SRV target of every
// service on every interface. Every matter.js MockNetwork interface is up
// and multicast-capable; "lo" is excluded by its zero MAC, as there.
func TestMdnsServerHostName(t *testing.T) {
	t.Parallel()
	up := net.FlagUp | net.FlagMulticast
	iface := func(mac string, addrs int) hostNameIface {
		hw, err := net.ParseMAC(mac)
		if err != nil {
			t.Fatal(err)
		}
		return hostNameIface{flags: up, mac: hw, addrs: addrs}
	}
	// beforeEach, :1749-1754.
	lo, eth0, docker0 := iface("00:00:00:00:00:00", 1), iface("aa:bb:cc:dd:ee:01", 1), iface("aa:bb:cc:dd:ee:02", 1)
	standard := func() []hostNameIface { return []hostNameIface{lo, eth0, docker0} }

	t.Run("names one host on every interface", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		anpi0 := iface("aa:bb:cc:dd:ee:00", 0)
		if got := c.choose(append([]hostNameIface{anpi0}, standard()...), true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
	t.Run("keeps the name while its MAC is present, even when another interface lists first", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		c.choose(standard(), true)
		if got := c.choose([]hostNameIface{docker0, eth0}, true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
	t.Run("follows a rotated MAC", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		c.choose(standard(), true)
		if got := c.choose([]hostNameIface{lo, iface("aa:bb:cc:dd:ee:03", 1), docker0}, true); got != "AABBCCDDEE030000" {
			t.Errorf("host = %q, want AABBCCDDEE030000", got)
		}
	})
	t.Run("skips an interface whose details cannot be read", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		gone := iface("aa:bb:cc:dd:ee:09", 1)
		gone.err = errors.New("interface gone is gone")
		if got := c.choose(append([]hostNameIface{gone}, standard()...), true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
}
