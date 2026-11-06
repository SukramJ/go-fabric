// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The Responder's own contract: the Advertiser surface, the broadcast
// schedule and its events, re-publishing, and closing.

func TestResponder_PublishWithdrawActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, _ := newWiredResponder(serverAddrs(true))
	svc := operationalFixture(port1)
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	active := r.Active()
	if len(active) != 1 || active[0].InstanceName != svc.InstanceName {
		t.Fatalf("Active = %+v", active)
	}
	if active[0].Addresses == nil && len(primaryHostIPs(nil)) > 0 {
		t.Error("Active does not carry the advertised addresses")
	}
	if err := r.Withdraw(ctx, svc.InstanceName, svc.ServiceType); err != nil {
		t.Fatal(err)
	}
	if len(r.Active()) != 0 {
		t.Error("Withdraw left the service active")
	}
	if err := r.Withdraw(ctx, svc.InstanceName, svc.ServiceType); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("second Withdraw = %v, want ErrServiceNotFound", err)
	}
	// After the goodbye nothing is answered for it.
	w.take()
	deliver(t, r, queryMsg([]dns.Question{question("_matter._tcp.local", dns.TypePTR, false)}), 1, dummySource)
	expectSent(t, w.take(), 0)
	if err := r.Publish(ctx, Service{}); err == nil {
		t.Error("Publish of an invalid service succeeded")
	}
}

func TestResponder_CloseIsIdempotentAndFinal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, _ := newWiredResponder(serverAddrs(true))
	if err := r.Publish(ctx, operationalFixture(port1)); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close = %v", err)
	}
	if len(r.Active()) != 0 {
		t.Error("Close left services active")
	}
	if err := r.Publish(ctx, operationalFixture(port1)); err == nil {
		t.Error("Publish after Close succeeded")
	}
	w.take()
	r.ServiceConnected("x", ServiceTypeOperational)
	r.ServiceDisconnected("x", ServiceTypeOperational)
	r.ServiceRetransmission("x", ServiceTypeOperational)
	expectSent(t, w.take(), 0)
	// A responder that never published closes without sockets.
	if err := NewResponder().Close(); err != nil {
		t.Errorf("Close of an unused responder = %v", err)
	}
}

// TestResponder_BroadcastSchedule: matter.js's DefaultBroadcastSchedule —
// broadcasts 1 s, 2 s, 4 s … apart, capped at 90 s — until a connection
// ends the run (broadcastAfterConnection Instant); a disconnect starts it
// again from 1 s; a retransmission broadcasts once when no run is under
// way.
func TestResponder_BroadcastSchedule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, clk := newWiredResponder(serverAddrs(true))
	svc := operationalFixture(port1)
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	expectSent(t, w.take(), 1)
	for _, gap := range []time.Duration{1, 2, 4, 8, 16, 32, 64, 90, 90} {
		clk.Advance(gap*time.Second - time.Millisecond)
		expectSent(t, w.take(), 0)
		clk.Advance(time.Millisecond)
		expectSent(t, w.take(), 1)
	}

	// An unchanged re-Publish does not reset a running schedule
	// (Advertisement.start) and says no goodbye.
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	expectSent(t, w.take(), 0)

	r.ServiceConnected(svc.InstanceName, svc.ServiceType)
	clk.Advance(10 * time.Minute)
	expectSent(t, w.take(), 0)

	// A retransmission while idle: one broadcast, then the run is over
	// after its 1 s.
	r.ServiceRetransmission(svc.InstanceName, svc.ServiceType)
	expectSent(t, w.take(), 1)
	r.ServiceRetransmission(svc.InstanceName, svc.ServiceType)
	expectSent(t, w.take(), 0)
	clk.Advance(time.Second)
	expectSent(t, w.take(), 0)
	r.ServiceRetransmission(svc.InstanceName, svc.ServiceType)
	expectSent(t, w.take(), 1)
	clk.Advance(time.Second)

	r.ServiceDisconnected(svc.InstanceName, svc.ServiceType)
	expectSent(t, w.take(), 1)
	clk.Advance(time.Second)
	expectSent(t, w.take(), 1)
	clk.Advance(2 * time.Second)
	expectSent(t, w.take(), 1)

	// An unknown service is ignored.
	r.ServiceDisconnected("nope", ServiceTypeOperational)
	expectSent(t, w.take(), 0)
}

// TestResponder_ScheduleEndsAtTheCommissioningTimeout: the default run
// lasts the maximum commissioning timeout, two days, then stops; the
// service keeps answering.
func TestResponder_ScheduleEndsAtTheCommissioningTimeout(t *testing.T) {
	t.Parallel()
	sched := newRetrySchedule(defaultBroadcastSchedule)
	sched.random32 = func() uint32 { return 0 }
	var total time.Duration
	n := 0
	for {
		d, ok := sched.next()
		if !ok {
			break
		}
		total += d
		n++
	}
	if total != 48*time.Hour {
		t.Errorf("schedule spans %v, want 48h", total)
	}
	// 1+2+…+64 = 127 s, then 90 s steps; the last one shortened to fit.
	if want := 7 + 1 + int((48*time.Hour-127*time.Second-1)/(90*time.Second)); n != want {
		t.Errorf("%d broadcasts, want %d", n, want)
	}
	// Jitter is at most a quarter of the base interval and the 90 s cap
	// includes it (RetrySchedule.ts:44-53).
	j := newRetrySchedule(defaultBroadcastSchedule)
	j.random32 = func() uint32 { return 1<<32 - 1 }
	if d, _ := j.next(); d <= time.Second || d >= 1250*time.Millisecond {
		t.Errorf("first jittered interval %v, want in (1s, 1.25s)", d)
	}
	for range 10 {
		if d, _ := j.next(); d > 90*time.Second {
			t.Errorf("interval %v exceeds the 90 s cap", d)
		}
	}
}

// TestResponder_RepublishChangedSaysGoodbyeFirst: a changed record set
// is announced in one message that first says goodbye to the record that
// left — the old TXT, without the cache-flush bit as its set still has a
// record — and then carries the new set (InterfaceResponder.ts:445-490 at
// db021da0; MdnsServerTest.ts "announces a changed record on the broadcast
// after a refresh and says goodbye to its old value").
func TestResponder_RepublishChangedSaysGoodbyeFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, _ := newWiredResponder(serverAddrs(true))
	svc := operationalFixture(port1)
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	w.take()
	svc.TXT = append(svc.TXT, TXTRecord{Key: "ICD", Value: "1"})
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	got := w.take()
	expectSent(t, got, 1)
	answers := got[0].msg.Answer
	if len(answers) < 2 || answers[0].Header().Ttl != 0 || answers[0].Header().Rrtype != dns.TypeTXT ||
		answers[0].Header().Class&classCacheFlush != 0 {
		t.Fatalf("the message does not open with the old TXT's goodbye: %v", answers)
	}
	for _, rr := range answers[1:] {
		if rr.Header().Ttl != recordTTL {
			t.Errorf("%v is not announced", rr)
		}
	}
	if len(r.Active()) != 1 {
		t.Errorf("Active = %d services, want 1", len(r.Active()))
	}
}

// TestResponder_TriggerReannounce announces every service now, in one
// message per interface (MdnsServer.broadcast, MdnsServer.ts:210-223 at
// db021da0).
func TestResponder_TriggerReannounce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, clk := newWiredResponder(serverAddrs(true))
	for _, svc := range []Service{operationalFixture(port1), commissionableFixture(port2)} {
		if err := r.Publish(ctx, svc); err != nil {
			t.Fatal(err)
		}
	}
	w.take()
	r.TriggerReannounce(ctx)
	// One publication, split into datagrams by size.
	var srvs int
	for _, p := range w.take() {
		for _, rr := range p.msg.Answer {
			if rr.Header().Rrtype == dns.TypeSRV {
				srvs++
			}
		}
	}
	if srvs != 2 {
		t.Errorf("the announcement carries %d SRV records, want both services'", srvs)
	}
	clk.Advance(120 * time.Millisecond)
	expectSent(t, w.take(), 0)
}

// TestResponder_HostQuestions: the bare host-name questions — TC-SC-4.1 and
// TC-SC-4.3 resolve the SRV target with an AAAA query of their own — are
// answered from the interface's address records, IPv6 in selection
// preference order, each 120 s with the cache-flush bit; an A/AAAA-only
// question gets no additionals; a mixed query gets the rest of the set.
func TestResponder_HostQuestions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, _ := newWiredResponder(map[int][]string{1: {"2001:db8::7", "fd00::1", "fe80::1", "192.168.1.10"}})
	if err := r.Publish(ctx, operationalFixture(port1)); err != nil {
		t.Fatal(err)
	}
	w.take()
	host := serverHost + ".local."
	deliver(t, r, queryMsg([]dns.Question{question(host, dns.TypeAAAA, false)}), 1, dummySource)
	got := w.take()
	expectSent(t, got, 1)
	expectResponse(t, got[0], 1, nil, []dns.RR{
		flushed(addrRR(host, "fe80::1")), flushed(addrRR(host, "fd00::1")), flushed(addrRR(host, "2001:db8::7")),
	}, nil)
	// A mixed query carries the rest of the set (except the PTRs) as
	// additionals.
	deliver(t, r, queryMsg([]dns.Question{question(host, dns.TypeA, false), question("other.local", dns.TypeSRV, false)}), 1, dummySource)
	got = w.take()
	expectSent(t, got, 1)
	if len(got[0].msg.Extra) != 5 {
		t.Errorf("additionals %v, want SRV, TXT and the three AAAA", rrStrings(got[0].msg.Extra))
	}
	// Another interface without addresses is not answered on.
	deliver(t, r, queryMsg([]dns.Question{question(host, dns.TypeAAAA, false)}), 2, dummySource)
	expectSent(t, w.take(), 0)
}

// TestResponder_QUThenQMGetsOneMulticast: the python-zeroconf resolver the
// CHIP cases use sends a QU and then a QM copy of the same question back
// to back; the link gets one multicast answer.
func TestResponder_QUThenQMGetsOneMulticast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, w, _ := newWiredResponder(serverAddrs(false))
	if err := r.Publish(ctx, operationalFixture(port1)); err != nil {
		t.Fatal(err)
	}
	w.take()
	host := serverHost + ".local."
	deliver(t, r, queryMsg([]dns.Question{question(host, dns.TypeAAAA, true)}), 1, dummySource)
	deliver(t, r, queryMsg([]dns.Question{question(host, dns.TypeAAAA, false)}), 1, dummySource)
	got := w.take()
	expectSent(t, got, 1)
	if got[0].dst != nil {
		t.Errorf("first answer unicast to %v, want multicast", got[0].dst)
	}
}

// TestResponder_ConcurrentUse drives Publish, Withdraw, queries, events and
// the schedule's timers from several goroutines and closes in the middle —
// the race detector arbitrates.
func TestResponder_ConcurrentUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, _, clk := newWiredResponder(serverAddrs(true))
	svc := operationalFixture(port1)
	pkt, err := queryMsg([]dns.Question{question("_matter._tcp.local", dns.TypePTR, false)}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				_ = r.Publish(ctx, svc)
				r.receive(pkt, 1, dummySource)
				r.ServiceDisconnected(svc.InstanceName, svc.ServiceType)
				r.ServiceRetransmission(svc.InstanceName, svc.ServiceType)
				r.TriggerReannounce(ctx)
				_ = r.Active()
				_ = r.Withdraw(ctx, svc.InstanceName, svc.ServiceType)
			}
		})
	}
	wg.Go(func() {
		for range 50 {
			clk.Advance(500 * time.Millisecond)
		}
	})
	wg.Go(func() { _ = r.Close() })
	wg.Wait()
	clk.Advance(time.Hour)
}
