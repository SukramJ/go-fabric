// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// fakeConn is an in-memory mdns.QueryConn: it records what is sent and
// delivers what a test injects.
type fakeConn struct {
	mu     sync.Mutex
	sent   [][]byte
	out    chan mdns.QueryResponse
	closed bool
	onSend func(msg []byte)
}

func newFakeConn() *fakeConn { return &fakeConn{out: make(chan mdns.QueryResponse, 256)} }

func (f *fakeConn) Send(msg []byte) error {
	f.mu.Lock()
	f.sent = append(f.sent, slices.Clone(msg))
	cb := f.onSend
	f.mu.Unlock()
	if cb != nil {
		cb(msg)
	}
	return nil
}

// queries decodes every sent message.
func (f *fakeConn) queries(t *testing.T) []mdns.DNSMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]mdns.DNSMessage, 0, len(f.sent))
	for _, b := range f.sent {
		m, err := mdns.ParseDNSMessage(b)
		if err != nil {
			t.Fatalf("sent message does not parse: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func (f *fakeConn) reset() {
	f.mu.Lock()
	f.sent = nil
	f.mu.Unlock()
}

func (f *fakeConn) Responses() <-chan mdns.QueryResponse { return f.out }

func (f *fakeConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

// fakeClock fires timers only when a test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// advance moves the clock forward by d, firing each due timer in time
// order and letting the loop process what it posted before the next.
func (c *fakeClock) advance(n *Names, d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if t.stopped || t.fired || t.at.After(target) {
				continue
			}
			if next == nil || t.at.Before(next.at) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			_ = n.loop.call(func() {})
			return
		}
		if next.at.After(c.now) {
			c.now = next.at
		}
		next.fired = true
		c.mu.Unlock()
		next.f()
		_ = n.loop.call(func() {})
	}
}

// testNames builds a cache on a fake connection and a fake clock.
func testNames(t *testing.T) (*Names, *fakeConn, *fakeClock) {
	t.Helper()
	conn := newFakeConn()
	clk := newFakeClock()
	n := newNames(conn, nil, namesConfig{
		clock:         clk,
		random:        func() uint32 { return 0 },
		interfaceName: func(i int) string { return map[int]string{0: "", 7: "eth7"}[i] },
	})
	t.Cleanup(func() { _ = n.Close() })
	return n, conn, clk
}

// deliver feeds one response packet to the cache and waits until it is
// processed.
func deliver(t *testing.T, n *Names, ifIndex int, answers []dns.RR, extra ...dns.RR) {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Answer = answers
	m.Extra = extra
	b, err := m.Pack()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := n.loop.call(func() { n.handleMessage(mdns.QueryResponse{Msg: b, IfIndex: ifIndex}) }); err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

func hdr(name string, t uint16, ttl uint32, flush bool) dns.RR_Header {
	class := uint16(dns.ClassINET)
	if flush {
		class |= 0x8000
	}
	return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: t, Class: class, Ttl: ttl}
}

func srvRR(name, target string, port uint16, ttl uint32, flush bool) dns.RR {
	return &dns.SRV{Hdr: hdr(name, dns.TypeSRV, ttl, flush), Target: dns.Fqdn(target), Port: port}
}

func txtRR(name string, ttl uint32, flush bool, entries ...string) dns.RR {
	return &dns.TXT{Hdr: hdr(name, dns.TypeTXT, ttl, flush), Txt: entries}
}

func aRR(name, ip string, ttl uint32, flush bool) dns.RR {
	return &dns.A{Hdr: hdr(name, dns.TypeA, ttl, flush), A: net.ParseIP(ip).To4()}
}

func aaaaRR(name, ip string, ttl uint32, flush bool) dns.RR {
	return &dns.AAAA{Hdr: hdr(name, dns.TypeAAAA, ttl, flush), AAAA: net.ParseIP(ip)}
}

// onLoop runs fn on the cache's loop.
func onLoop(t *testing.T, n *Names, fn func()) {
	t.Helper()
	if err := n.loop.call(fn); err != nil {
		t.Fatalf("loop: %v", err)
	}
}

func mdnsResponse(b []byte, ifIndex int) mdns.QueryResponse {
	return mdns.QueryResponse{Msg: b, IfIndex: ifIndex}
}
