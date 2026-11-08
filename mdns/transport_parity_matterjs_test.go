// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// logRecorder keeps every record's level and message.
type logRecorder struct {
	mu   *sync.Mutex
	recs *[]slog.Record
}

func newLogRecorder() logRecorder {
	return logRecorder{mu: &sync.Mutex{}, recs: &[]slog.Record{}}
}

func (h logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h logRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	*h.recs = append(*h.recs, r)
	h.mu.Unlock()
	return nil
}
func (h logRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h logRecorder) WithGroup(string) slog.Handler      { return h }

// at returns the messages logged at level.
func (h logRecorder) at(level slog.Level) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range *h.recs {
		if r.Level == level {
			out = append(out, r.Message)
		}
	}
	return out
}

// TestMdnsServerTransportNotices ports the remaining cases of matter.js
// packages/protocol/test/mdns/MdnsServerTransportTest.ts at db021da0 by
// meaning, on the fake wire: the once-per-outage notice of a missed
// multicast — Info here, matter.js's logger.notice — the deferred answer of
// the one-second rule, the per-datagram reach of a split message, a send
// with no usable family, the interface state a waiting retry keeps, and
// the addresses an interface gains (InterfaceResponder.ts #transmit,
// #sendFailed, #multicastMissed, #holdBack, #sendDeferred, prune; the
// record cache's 15 minutes).
func TestMdnsServerTransportNotices(t *testing.T) {
	t.Parallel()
	controller := &net.UDPAddr{IP: net.ParseIP("192.168.200.2"), Port: 5353}
	failAll := func(*dns.Msg) reach { return reachFailed }
	fixture := func(t *testing.T) (*Responder, *fakeWire, *fakeClock, logRecorder) {
		t.Helper()
		r, w, clk := newGoodbyeFixture(t)
		logs := newLogRecorder()
		r.SetLogger(slog.New(logs))
		return r, w, clk, logs
	}
	// query is the test's query (:197-203): the question, then 200 ms.
	query := func(r *Responder, clk *fakeClock, name string, qu bool, qtype uint16) {
		pkt, _ := queryMsg([]dns.Question{question(name, qtype, qu)}).Pack()
		r.receive(pkt, 1, controller)
		clk.Advance(200 * time.Millisecond)
	}
	notices := func(logs logRecorder) []string { return logs.at(slog.LevelInfo) }
	bigRecords := func() []dns.RR {
		rrs := gbServiceRecords("big.b.c.d.", recordTTL)
		for i := range 30 {
			rrs = append(rrs, txtRR("big.b.c.d.", "key"+strconv.Itoa(i)+"="+strings.Repeat("x", 40)))
		}
		return rrs
	}

	// :236-250.
	t.Run("notices a failing multicast answer once even when a unicast answer succeeds in between", func(t *testing.T) {
		t.Parallel()
		r, w, clk, logs := fixture(t)
		query(r, clk, gbOperational, false, dns.TypeSRV)
		clk.Advance(2 * time.Second)
		w.take()
		w.setFail(failAll)
		query(r, clk, gbOperational, false, dns.TypeSRV)
		expectStrings(t, "notices", notices(logs), []string{"mdns.responder.multicast_not_sent"})
		query(r, clk, gbOperational, true, dns.TypeSRV)
		got := w.take()
		if len(got) != 1 || got[0].dst == nil || !got[0].dst.IP.Equal(controller.IP) {
			t.Fatalf("sent %d packet(s), want the one unicast answer to the controller", len(got))
		}
		clk.Advance(2 * time.Second)
		query(r, clk, gbOperational, false, dns.TypeSRV)
		expectStrings(t, "notices", notices(logs), []string{"mdns.responder.multicast_not_sent"})
	})

	// :252-263.
	t.Run("notices a failing deferred answer", func(t *testing.T) {
		t.Parallel()
		r, w, clk, logs := fixture(t)
		query(r, clk, gbOperational, false, dns.TypeSRV)
		answeredAt := clk.Now().Add(-200 * time.Millisecond)
		w.take()
		var failedAt []time.Time
		w.setFail(func(*dns.Msg) reach { failedAt = append(failedAt, clk.Now()); return reachFailed })
		query(r, clk, gbOperational, false, dns.TypeSRV)
		clk.Advance(2 * time.Second)
		if len(failedAt) != 1 || failedAt[0].Sub(answeredAt) < time.Second {
			t.Fatalf("failed sends at %v, want one deferred answer at least a second after %v", failedAt, answeredAt)
		}
		expectStrings(t, "notices", notices(logs), []string{"mdns.responder.multicast_not_sent"})
	})

	// :291-308.
	t.Run("says goodbye to the records of an announcement whose later datagram failed", func(t *testing.T) {
		t.Parallel()
		r, w, clk, _ := fixture(t)
		setRecords(r, "big", bigRecords()...)
		clk.Advance(2 * time.Second)
		datagram := 0
		w.setFail(func(*dns.Msg) reach {
			datagram++
			if datagram == 2 {
				return reachFailed
			}
			return reachAll
		})
		broadcastKey(r, "big")
		if datagram < 2 {
			t.Fatalf("the announcement left in %d datagram(s), want a split", datagram)
		}
		w.setFail(nil)
		clk.Advance(2 * time.Second)
		w.take()
		withdrawKey(r, "big")
		var goodbyes int
		for _, rr := range goodbyesIn(w.take()) {
			if rr.Header().Name == "big.b.c.d." {
				goodbyes++
			}
		}
		if goodbyes == 0 {
			t.Fatal("no goodbye for the records of the announcement that reached some")
		}
	})

	// :310-327.
	t.Run("logs at debug level a unicast answer whose later datagram failed, and notices nothing", func(t *testing.T) {
		t.Parallel()
		r, w, clk, logs := fixture(t)
		setRecords(r, "big", bigRecords()...)
		query(r, clk, "big.b.c.d.", false, dns.TypeTXT)
		clk.Advance(2 * time.Second)
		w.take()
		datagram := 0
		w.mu.Lock()
		w.failUnicast = func(*dns.Msg) reach {
			datagram++
			if datagram == 2 {
				return reachFailed
			}
			return reachAll
		}
		w.mu.Unlock()
		query(r, clk, "big.b.c.d.", true, dns.TypeTXT)
		var toController int
		for _, p := range w.take() {
			if p.dst != nil && p.dst.IP.Equal(controller.IP) {
				toController++
			}
		}
		if toController != 1 {
			t.Fatalf("%d datagram(s) reached the controller, want 1", toController)
		}
		found := false
		for _, m := range logs.at(slog.LevelDebug) {
			found = found || m == "mdns.responder.response_part_not_sent"
		}
		if !found {
			t.Error("the failed part of the unicast answer was not logged at debug level")
		}
		expectStrings(t, "notices", notices(logs), nil)
	})

	// :350-365. matter.js checks at 16 min 5 s, where its retry still
	// waits by the drift of its 100 ms steps; the fake clock has none, so
	// the check runs past the interface's 15-minute lifetime while the
	// 16-minute record still needs its goodbye.
	t.Run("keeps the state of an interface whose goodbye retry waits, so close stops the retry", func(t *testing.T) {
		t.Parallel()
		r, w, clk, _ := fixture(t)
		setRecords(r, "long", gbServiceRecords("long.b.c.d.", 16*60)...)
		broadcastKey(r, "long")
		clk.Advance(2 * time.Second)
		w.setFail(failAll)
		withdrawKey(r, "foo", "window", "long")
		for range 15*60 + 30 {
			clk.Advance(time.Second)
		}
		r.mu.Lock()
		st, ok := r.linkStates[1]
		waiting := ok && st.retry != nil && st.retry.waiting
		r.mu.Unlock()
		if !waiting {
			t.Fatal("the interface state with its waiting retry is gone")
		}
		broadcastKey(r)
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if n := clk.Pending(); n != 0 {
			t.Fatalf("%d timer(s) armed after Close, want none", n)
		}
	})

	// :421-434.
	t.Run("does not try a goodbye again that failed while the server closes", func(t *testing.T) {
		t.Parallel()
		r, _, clk, _ := fixture(t)
		var tries int
		fw := r.wire.(*fakeWire)
		fw.setFail(func(*dns.Msg) reach { tries++; return reachFailed })
		withdrawKey(r, "window")
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		n := tries
		clk.Advance(5 * time.Second)
		if n == 0 || tries != n {
			t.Fatalf("%d attempts before, %d after; want some, then none", n, tries)
		}
	})

	// :457-474: no family usable — no notice, nothing cached, no goodbye.
	t.Run("neither notices nor says goodbye on an interface with only a family the server does not use", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newWiredResponder(map[int][]string{1: {dummyIP}})
		logs := newLogRecorder()
		r.SetLogger(slog.New(logs))
		w.setFail(func(*dns.Msg) reach { return reachNone })
		setRecords(r, "v6", gbServiceRecords("v6.b.c.d.", recordTTL)...)
		broadcastKey(r)
		w.setFail(nil)
		withdrawKey(r, "v6")
		r.mu.Lock()
		st := r.linkStates[1]
		waiting := st != nil && st.retry != nil
		r.mu.Unlock()
		if waiting {
			t.Fatal("a goodbye retry for records nobody cached")
		}
		clk.Advance(5 * time.Second)
		expectStrings(t, "notices", notices(logs), nil)
		if g := goodbyesIn(w.take()); len(g) != 0 {
			t.Fatalf("goodbyes %v for records that never left", summarize(g))
		}
	})

	// :476-491: the record cache is read again after 15 minutes.
	t.Run("publishes an address the interface gained after its details were read", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newWiredResponder(map[int][]string{1: {"192.168.200.1"}})
		if err := r.Publish(context.Background(), operationalFixture(port1)); err != nil {
			t.Fatal(err)
		}
		w.take()
		w.setAddrs(1, "192.168.200.1", "fd00::7")
		var found bool
		for range 4 {
			for range 5 * 60 {
				clk.Advance(time.Second)
			}
			w.take()
			r.TriggerReannounce(context.Background())
			for _, p := range w.take() {
				for _, rr := range p.msg.Answer {
					if a, ok := rr.(*dns.AAAA); ok && a.AAAA.Equal(net.ParseIP("fd00::7")) {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatal("the gained address was never published")
		}
	})

	// :511-527.
	t.Run("notices once per outage, whether multicasts reach some address families or none", func(t *testing.T) {
		t.Parallel()
		r, w, clk, logs := fixture(t)
		broadcastReaching := func(rc reach) {
			w.setFail(func(*dns.Msg) reach { return rc })
			broadcastKey(r)
			clk.Advance(2 * time.Second)
		}
		broadcastReaching(reachSome)
		broadcastReaching(reachFailed)
		broadcastReaching(reachSome)
		expectStrings(t, "notices", notices(logs), []string{"mdns.responder.multicast_some_families"})
		broadcastReaching(reachAll)
		broadcastReaching(reachFailed)
		expectStrings(t, "notices", notices(logs), []string{"mdns.responder.multicast_some_families", "mdns.responder.multicast_not_sent"})
	})

	// :541-557.
	t.Run("multicasts the answer to a QU query after a goodbye that left on IPv6 only", func(t *testing.T) {
		t.Parallel()
		r, w, clk, _ := fixture(t)
		w.setFail(func(*dns.Msg) reach { return reachSome })
		withdrawKey(r, "window")
		w.setFail(nil)
		setRecords(r, "window", gbServiceRecords(gbWindow, recordTTL)...)
		w.take()
		query(r, clk, gbWindow, true, dns.TypeSRV)
		clk.Advance(2 * time.Second)
		var answers int
		for _, p := range w.take() {
			for _, rr := range p.msg.Answer {
				if rr.Header().Name == gbWindow && rr.Header().Ttl > 0 {
					if p.dst != nil {
						t.Fatalf("answer unicast to %v, want multicast", p.dst)
					}
					answers++
				}
			}
		}
		if answers == 0 {
			t.Fatal("no answer for the window")
		}
	})
}
