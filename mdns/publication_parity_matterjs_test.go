// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The goodbye scope and the goodbye retry, ported by meaning from matter.js
// at db021da0 (#4844): the describe "Goodbye" of packages/protocol/test/
// mdns/MdnsServerTest.ts (:1663-2640), the retry cases of
// MdnsServerTransportTest.ts (:215-541) and the goodbye-scope cases of
// MdnsTest.ts (:909-1000). The fake wire stands in for the UDP transport;
// its failure rule is the test network's failing multicast (reachNone) and
// its IPv4-failing multicast on an interface with IPv6 (reachSome).

const (
	gbHost        = "abcd.local."
	gbDummy       = "_matter._tcp.local."
	gbOperational = "operational.b.c.d."
	gbWindow      = "window.b.c.d."
	gbNextWindow  = "next-window.b.c.d."
)

// gbServiceRecords is the test's serviceRecords: the type pointer, the SRV
// and the shared host address, each 120 s.
func gbServiceRecords(qname string, ttl uint32) []dns.RR {
	return []dns.RR{
		withTTL(ptrRR(gbDummy, qname), ttl),
		withTTL(srvRec(qname, 1234, gbHost), ttl),
		withTTL(addrRR(gbHost, dummyIP), ttl),
	}
}

func withTTL(rr dns.RR, ttl uint32) dns.RR {
	c := dns.Copy(rr)
	c.Header().Ttl = ttl
	return c
}

// setGen registers a generator fixture called on every record rebuild.
func setGen(r *Responder, key string, gen func() []dns.RR) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.services[key]; !ok {
		r.order = append(r.order, key)
	}
	r.services[key] = &entry{key: key, gen: gen}
	r.resetLocked()
}

// withdrawKey withdraws a fixture (Registration.withdraw).
func withdrawKey(r *Responder, keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range keys {
		if e, ok := r.services[key]; ok {
			r.removeLocked(e)
		}
	}
}

// broadcastKey publishes the named fixtures, or every one with none named
// (MdnsServer.broadcast).
func broadcastKey(r *Responder, keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ifIndex := range r.wire.links() {
		if len(keys) == 0 {
			r.publishLocked(ifIndex, nil, true)
			continue
		}
		named := map[string]bool{}
		for _, k := range keys {
			named[k] = true
		}
		r.publishLocked(ifIndex, named, false)
	}
}

// refresh rebuilds the records from the generators (Registration.refresh).
func refresh(r *Responder) {
	r.mu.Lock()
	r.resetLocked()
	r.mu.Unlock()
}

// newGoodbyeFixture is the describe's beforeEach: "foo" and "window"
// registered and announced, a second passed.
func newGoodbyeFixture(t *testing.T) (*Responder, *fakeWire, *fakeClock) {
	t.Helper()
	r, w, clk := newWiredResponder(map[int][]string{1: {dummyIP}})
	setRecords(r, "foo", gbServiceRecords(gbOperational, recordTTL)...)
	setRecords(r, "window", gbServiceRecords(gbWindow, recordTTL)...)
	broadcastKey(r)
	clk.Advance(time.Second)
	w.take()
	return r, w, clk
}

func goodbyesIn(sent []sentPacket) []dns.RR {
	var out []dns.RR
	for _, p := range sent {
		for _, rr := range p.msg.Answer {
			if rr.Header().Ttl == 0 {
				out = append(out, rr)
			}
		}
	}
	return out
}

// summarize is the test's summarize: "TYPE name ttl=N".
func summarize(rrs []dns.RR) []string {
	out := make([]string, 0, len(rrs))
	for _, rr := range rrs {
		h := rr.Header()
		out = append(out, dns.TypeToString[h.Rrtype]+" "+h.Name+" ttl="+strconv.FormatUint(uint64(h.Ttl), 10))
	}
	return out
}

func hostGoodbyes(rrs []dns.RR) []dns.RR {
	var out []dns.RR
	for _, rr := range rrs {
		if strings.EqualFold(rr.Header().Name, gbHost) && rr.Header().Rrtype == dns.TypeA {
			out = append(out, rr)
		}
	}
	return out
}

func expectStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got  %q\n want %q", what, got, want)
	}
}

// TestMdnsServerGoodbye is the describe "Goodbye" of MdnsServerTest.ts
// (:1663-2640 at db021da0) for the goodbye scope: a goodbye names only the
// records no remaining service publishes (InterfaceResponder.ts
// #goodbyesFor, :492-510), in the publication that announces the rest
// (:445-490).
func TestMdnsServerGoodbye(t *testing.T) {
	t.Parallel()

	t.Run("omits the host addresses another service still publishes", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		withdrawKey(r, "window")
		expectStrings(t, "goodbyes", summarize(goodbyesIn(w.take())),
			[]string{"PTR " + gbDummy + " ttl=0", "SRV " + gbWindow + " ttl=0"})
	})

	t.Run("omits the host addresses a remaining service publishes when several services leave at once", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		setRecords(r, "next-window", gbServiceRecords(gbNextWindow, recordTTL)...)
		withdrawKey(r, "foo", "window")
		got := goodbyesIn(w.take())
		var srvs []string
		for _, rr := range got {
			if rr.Header().Rrtype == dns.TypeSRV {
				srvs = append(srvs, rr.Header().Name)
			}
		}
		sort.Strings(srvs)
		expectStrings(t, "withdrawn services", srvs, []string{gbOperational, gbWindow})
		if h := hostGoodbyes(got); len(h) != 0 {
			t.Errorf("host goodbyes %v", h)
		}
	})

	t.Run("includes the host addresses once no other service publishes them", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		withdrawKey(r, "window")
		w.take()
		withdrawKey(r, "foo")
		h := hostGoodbyes(goodbyesIn(w.take()))
		if len(h) != 1 || h[0].Header().Class&classCacheFlush == 0 {
			t.Errorf("host goodbyes %v, want the A record once with the cache-flush bit", h)
		}
	})

	t.Run("includes the host addresses once when the last services are withdrawn together", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		withdrawKey(r, "window", "foo")
		if h := hostGoodbyes(goodbyesIn(w.take())); len(h) != 1 {
			t.Errorf("host goodbyes %v, want one", h)
		}
	})

	t.Run("says goodbye to a record only once", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		withdrawKey(r, "window")
		w.take()
		withdrawKey(r, "foo")
		expectStrings(t, "goodbyes", summarize(goodbyesIn(w.take())), []string{
			"PTR " + gbDummy + " ttl=0", "SRV " + gbOperational + " ttl=0", "A " + gbHost + " ttl=0",
		})
	})

	t.Run("says goodbye to a replaced address without the cache-flush bit while its set has records", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		ip := "10.1.1.1"
		setGen(r, "moving", func() []dns.RR {
			return []dns.RR{srvRec("moving.b.c.d", 1234, "moving.local"), flushed(addrRR("moving.local", ip))}
		})
		broadcastKey(r, "moving")
		w.take()
		ip = "10.1.1.2"
		refresh(r)
		expectSent(t, w.take(), 0)
		broadcastKey(r, "moving")
		got := w.take()
		expectSent(t, got, 1)
		var moving []dns.RR
		for _, rr := range got[0].msg.Answer {
			if rr.Header().Name == "moving.local." {
				moving = append(moving, rr)
			}
		}
		want := []dns.RR{withTTL(addrRR("moving.local", "10.1.1.1"), 0), flushed(addrRR("moving.local", "10.1.1.2"))}
		expectStrings(t, "moving.local", rrStrings(moving), rrStrings(want))
	})

	t.Run("announces all services in one packet with each shared record once", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		broadcastKey(r)
		got := w.take()
		expectSent(t, got, 1)
		var hosts int
		for _, rr := range got[0].msg.Answer {
			if rr.Header().Name == gbHost {
				hosts++
			}
		}
		if hosts != 1 {
			t.Errorf("%d host records, want 1", hosts)
		}
	})

	t.Run("announces a changed record on the broadcast after a refresh and says goodbye to its old value", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		txt := "A=1"
		setGen(r, "txt", func() []dns.RR { return []dns.RR{txtRR("txt.b.c.d", txt)} })
		deliver(t, r, queryMsg([]dns.Question{question("txt.b.c.d", dns.TypeTXT, false)}), 1, dummySource)
		txt = "A=2"
		refresh(r)
		broadcastKey(r, "txt")
		got := w.take()
		expectSent(t, got, 2)
		expectStrings(t, "answer", rrStrings(got[0].msg.Answer), rrStrings([]dns.RR{txtRR("txt.b.c.d", "A=1")}))
		expectStrings(t, "publication", rrStrings(got[1].msg.Answer),
			rrStrings([]dns.RR{withTTL(txtRR("txt.b.c.d", "A=1"), 0), txtRR("txt.b.c.d", "A=2")}))
	})

	t.Run("says goodbye to the vendor pointer on the broadcast after a refresh masks it", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		masked := false
		setGen(r, "masked", func() []dns.RR {
			out := []dns.RR{srvRec("masked.b.c.d", 1234, gbHost)}
			if !masked {
				out = append([]dns.RR{ptrRR("_V1._sub._matterc._udp.local", "masked.b.c.d")}, out...)
			}
			return out
		})
		broadcastKey(r, "masked")
		w.take()
		masked = true
		refresh(r)
		broadcastKey(r, "masked")
		expectStrings(t, "goodbyes", summarize(goodbyesIn(w.take())), []string{"PTR _V1._sub._matterc._udp.local. ttl=0"})
	})

	t.Run("keeps answering after a goodbye whose send failed", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		w.setFail(func(m *dns.Msg) reach {
			if len(m.Answer) > 0 && m.Answer[0].Header().Ttl == 0 {
				return reachNone
			}
			return reachAll
		})
		withdrawKey(r, "window")
		deliver(t, r, queryMsg([]dns.Question{question(gbOperational, dns.TypeSRV, false)}), 1, dummySource)
		var answers []dns.RR
		for _, p := range w.take() {
			answers = append(answers, p.msg.Answer...)
		}
		expectStrings(t, "answers", summarize(answers), []string{"SRV " + gbOperational + " ttl=120"})
	})

	t.Run("sends no goodbye for records that peers no longer cache", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		setRecords(r, "short", withTTL(srvRec("short.b.c.d", 1234, gbHost), 2))
		broadcastKey(r, "short")
		clk.Advance(3 * time.Second)
		w.take()
		withdrawKey(r, "short")
		if g := goodbyesIn(w.take()); len(g) != 0 {
			t.Errorf("goodbyes %v", g)
		}
	})

	t.Run("says goodbye to a record that went out only as an additional record", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		setRecords(r, "extra", srvRec("extra.b.c.d", 1234, "extra.local"), addrRR("extra.local", "10.2.2.2"))
		deliver(t, r, queryMsg([]dns.Question{question("extra.b.c.d", dns.TypeSRV, false)}), 1, dummySource)
		w.take()
		withdrawKey(r, "extra")
		expectStrings(t, "goodbyes", summarize(goodbyesIn(w.take())),
			[]string{"SRV extra.b.c.d. ttl=0", "A extra.local. ttl=0"})
	})

	t.Run("keeps saying goodbye for a record whose TTL outlasts the idle time of the interface", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		setRecords(r, "long", withTTL(srvRec("long.b.c.d", 1234, gbHost), 30*60))
		broadcastKey(r, "long")
		clk.Advance(16 * time.Minute)
		w.take()
		withdrawKey(r, "long")
		expectStrings(t, "goodbyes", summarize(goodbyesIn(w.take())), []string{"SRV long.b.c.d. ttl=0"})
	})

	t.Run("sends no packet when there is nothing to announce or say goodbye to", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		broadcastKey(r, "unknown")
		expectSent(t, w.take(), 0)
	})
}

// TestMdnsServerTransport ports the goodbye retry of
// MdnsServerTransportTest.ts (:215-541 at db021da0): a goodbye that did not
// leave is sent again a second later, then at twice the interval up to a
// minute, with only the goodbyes, until it leaves or peers dropped the
// records (InterfaceResponder.ts #retryGoodbyes and #settleGoodbyes,
// :512-547).
func TestMdnsServerTransport(t *testing.T) {
	t.Parallel()
	failAll := func(*dns.Msg) reach { return reachNone }
	goodbyesOf := func(sent []sentPacket, qname string) []dns.RR {
		var out []dns.RR
		for _, rr := range goodbyesIn(sent) {
			if p, ok := rr.(*dns.PTR); rr.Header().Name == qname || (ok && p.Ptr == qname) {
				out = append(out, rr)
			}
		}
		return out
	}
	retryWaits := func(r *Responder) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		st, ok := r.linkStates[1]
		return ok && st.retry != nil && st.retry.waiting
	}
	t.Run("sends a goodbye that failed with the next publication", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(failAll)
		withdrawKey(r, "window")
		w.setFail(nil)
		broadcastKey(r)
		clk.Advance(2 * time.Second)
		goodbyes := goodbyesOf(w.take(), gbWindow)
		types := make([]string, 0, len(goodbyes))
		for _, rr := range goodbyes {
			types = append(types, dns.TypeToString[rr.Header().Rrtype])
		}
		sort.Strings(types)
		expectStrings(t, "goodbyes of window", types, []string{"PTR", "SRV"})
	})

	t.Run("tries a failed goodbye of the last service again without a further publication", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(failAll)
		withdrawKey(r, "foo", "window")
		w.setFail(nil)
		clk.Advance(5 * time.Second)
		got := w.take()
		if len(goodbyesOf(got, gbWindow)) == 0 || len(goodbyesOf(got, gbOperational)) == 0 {
			t.Errorf("retried goodbyes missing:%v", rrStrings(goodbyesIn(got)))
		}
	})

	t.Run("stops trying a failed goodbye once peers no longer cache its records", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		var tries int
		w.setFail(func(*dns.Msg) reach { tries++; return reachNone })
		withdrawKey(r, "foo", "window")
		for range 120 {
			clk.Advance(time.Second)
		}
		n := tries
		if n <= 2 {
			t.Fatalf("%d attempts within the TTL", n)
		}
		for range 120 {
			clk.Advance(time.Second)
		}
		if tries != n {
			t.Errorf("%d attempts after the TTL ended, want none", tries-n)
		}
		if retryWaits(r) {
			t.Error("a retry still waits")
		}
	})

	t.Run("ends the retry once the goodbyes left", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(failAll)
		withdrawKey(r, "window")
		if !retryWaits(r) {
			t.Fatal("no retry waits")
		}
		w.setFail(nil)
		clk.Advance(3 * time.Second)
		if len(goodbyesOf(w.take(), gbWindow)) == 0 {
			t.Error("the goodbye did not leave")
		}
		if retryWaits(r) {
			t.Error("the retry did not end")
		}
	})

	t.Run("starts no retry for a failed announcement without goodbyes", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newGoodbyeFixture(t)
		w.setFail(failAll)
		broadcastKey(r)
		if retryWaits(r) {
			t.Error("a retry waits")
		}
	})

	t.Run("tries a failed goodbye again at most a minute apart", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		setRecords(r, "long", gbServiceRecords("long.b.c.d.", 600)...)
		broadcastKey(r, "long")
		clk.Advance(2 * time.Second)
		var failedAt []time.Time
		w.setFail(func(*dns.Msg) reach { failedAt = append(failedAt, clk.Now()); return reachNone })
		withdrawKey(r, "long")
		for range 2500 {
			clk.Advance(100 * time.Millisecond)
		}
		var gaps []string
		for i := 1; i < len(failedAt); i++ {
			gaps = append(gaps, failedAt[i].Sub(failedAt[i-1]).Round(time.Second).String())
		}
		expectStrings(t, "gaps", gaps, []string{"1s", "2s", "4s", "8s", "16s", "32s", "1m0s", "1m0s", "1m0s"})
	})

	t.Run("keeps the pending retry when another goodbye fails meanwhile", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		var failedAt []time.Time
		w.setFail(func(*dns.Msg) reach { failedAt = append(failedAt, clk.Now()); return reachNone })
		withdrawKey(r, "window")
		clk.Advance(500 * time.Millisecond)
		withdrawKey(r, "foo")
		for range 80 {
			clk.Advance(100 * time.Millisecond)
		}
		retries := failedAt[2:]
		var gaps []string
		for i := 1; i < len(retries); i++ {
			gaps = append(gaps, retries[i].Sub(retries[i-1]).Round(time.Second).String())
		}
		expectStrings(t, "retry gaps", gaps, []string{"2s", "4s"})
	})

	t.Run("sends only the goodbyes when it tries them again", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(failAll)
		withdrawKey(r, "window")
		clk.Advance(2 * time.Second)
		w.take()
		w.setFail(nil)
		clk.Advance(3 * time.Second)
		var records []dns.RR
		for _, p := range w.take() {
			records = append(records, p.msg.Answer...)
		}
		if len(records) == 0 {
			t.Fatal("nothing retried")
		}
		for _, rr := range records {
			if rr.Header().Ttl > 0 {
				t.Errorf("the retry carries %v", rr)
			}
		}
	})

	t.Run("does not try a failed goodbye again after close", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		var tries int
		w.setFail(func(*dns.Msg) reach { tries++; return reachNone })
		withdrawKey(r, "window")
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		n := tries
		if retryWaits(r) {
			t.Error("a retry waits after close")
		}
		clk.Advance(5 * time.Second)
		if tries != n {
			t.Errorf("%d attempts after close", tries-n)
		}
	})

	t.Run("tries a failed goodbye again after a second once an earlier retry ended", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(failAll)
		withdrawKey(r, "window")
		clk.Advance(8 * time.Second)
		w.setFail(nil)
		clk.Advance(70 * time.Second)
		if len(goodbyesOf(w.take(), gbWindow)) == 0 {
			t.Fatal("the first goodbye did not leave")
		}
		var tries int
		w.setFail(func(*dns.Msg) reach { tries++; return reachNone })
		withdrawKey(r, "foo")
		n := tries
		clk.Advance(1500 * time.Millisecond)
		if tries != n+1 {
			t.Errorf("%d attempts within 1.5 s of the withdrawal, want one retry", tries-n)
		}
	})

	t.Run("says goodbye to records whose announcement left on IPv6 only", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(func(*dns.Msg) reach { return reachSome })
		setRecords(r, "third", gbServiceRecords("third.b.c.d.", recordTTL)...)
		broadcastKey(r, "third")
		w.setFail(nil)
		clk.Advance(2 * time.Second)
		w.take()
		withdrawKey(r, "third")
		if len(goodbyesOf(w.take(), "third.b.c.d.")) == 0 {
			t.Error("no goodbye for the records that left on IPv6 only")
		}
	})

	t.Run("tries a goodbye that left on IPv6 only again until it leaves on IPv4", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newGoodbyeFixture(t)
		w.setFail(func(*dns.Msg) reach { return reachSome })
		withdrawKey(r, "window")
		if len(goodbyesOf(w.take(), gbWindow)) == 0 {
			t.Fatal("the goodbye did not leave on IPv6")
		}
		w.setFail(nil)
		clk.Advance(3 * time.Second)
		if len(goodbyesOf(w.take(), gbWindow)) == 0 {
			t.Error("the goodbye was not tried again")
		}
		if retryWaits(r) {
			t.Error("the retry did not end once the goodbye reached every family")
		}
	})
}

// TestMdnsGoodbyeScopeAcrossAdvertisements ports the goodbye-scope cases of
// MdnsTest.ts (:909-950 at db021da0) on the services the Build* helpers
// make: one service's goodbye keeps the host addresses and the shared
// service-type pointers the remaining ones publish, and the last one's
// goodbye says goodbye to them.
func TestMdnsGoodbyeScopeAcrossAdvertisements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	goodbyeOfService := func(t *testing.T, r *Responder, w *fakeWire, svc Service) []dns.RR {
		t.Helper()
		w.take()
		if err := r.Withdraw(ctx, svc.InstanceName, svc.ServiceType); err != nil {
			t.Fatal(err)
		}
		return goodbyesIn(w.take())
	}
	hostRecordsOf := func(rrs []dns.RR) []string {
		var out []string
		for _, rr := range rrs {
			switch a := rr.(type) {
			case *dns.A:
				out = append(out, "A "+a.A.String())
			case *dns.AAAA:
				out = append(out, "AAAA "+a.AAAA.String())
			}
		}
		sort.Strings(out)
		return out
	}
	hasPTR := func(rrs []dns.RR, name, target string) bool {
		for _, rr := range rrs {
			if p, ok := rr.(*dns.PTR); ok && p.Hdr.Name == name && p.Ptr == target {
				return true
			}
		}
		return false
	}
	countSRV := func(rrs []dns.RR) int {
		n := 0
		for _, rr := range rrs {
			if rr.Header().Rrtype == dns.TypeSRV {
				n++
			}
		}
		return n
	}
	const services = "_services._dns-sd._udp.local."
	publish := func(t *testing.T, r *Responder, svcs ...Service) {
		t.Helper()
		for _, svc := range svcs {
			if err := r.Publish(ctx, svc); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("keeps the host addresses when a commissionable advertisement ends and the operational one stays", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		commissionable := commissionableFixture(port1)
		publish(t, r, operationalFixture(port1), commissionable)
		g := goodbyeOfService(t, r, w, commissionable)
		if h := hostRecordsOf(g); len(h) != 0 {
			t.Errorf("host goodbyes %v", h)
		}
		if !hasPTR(g, services, "_matterc._udp.local.") {
			t.Errorf("no goodbye for the commissionable service type: %v", rrStrings(g))
		}
	})

	t.Run("keeps the host addresses and the service type when one of two operational advertisements ends", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		second := operationalFixture(port1)
		second.InstanceName = "0000000000000002-8080808080808080"
		publish(t, r, operationalFixture(port1), second)
		g := goodbyeOfService(t, r, w, second)
		if h := hostRecordsOf(g); len(h) != 0 {
			t.Errorf("host goodbyes %v", h)
		}
		if hasPTR(g, services, "_matter._tcp.local.") {
			t.Error("goodbye for the shared service type")
		}
		if n := countSRV(g); n != 1 {
			t.Errorf("%d SRV goodbyes, want 1", n)
		}
	})

	t.Run("keeps the shared service type pointers when one of two commissionable advertisements ends", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		first, second := commissionableFixture(port1), commissionableFixture(port2)
		second.InstanceName = "8181818181818181"
		publish(t, r, first, second)
		g := goodbyeOfService(t, r, w, second)
		for _, rr := range g {
			if rr.Header().Name == services {
				t.Errorf("goodbye for the shared pointer %v", rr)
			}
		}
		if h := hostRecordsOf(g); len(h) != 0 {
			t.Errorf("host goodbyes %v", h)
		}
		if n := countSRV(g); n != 1 {
			t.Errorf("%d SRV goodbyes, want 1", n)
		}
	})

	t.Run("says goodbye to the host addresses and the service type once the last advertisement ends", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		operational, commissionable := operationalFixture(port1), commissionableFixture(port1)
		publish(t, r, operational, commissionable)
		goodbyeOfService(t, r, w, commissionable)
		g := goodbyeOfService(t, r, w, operational)
		expectStrings(t, "host goodbyes", hostRecordsOf(g), []string{"A " + serverIPv4, "AAAA " + serverIPv6})
		if !hasPTR(g, services, "_matter._tcp.local.") {
			t.Errorf("no goodbye for the operational service type: %v", rrStrings(g))
		}
	})
}
