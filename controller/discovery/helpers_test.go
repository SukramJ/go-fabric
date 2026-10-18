// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock/clocktest"
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

// fakeClock is the test clock, a [clocktest.Fake]: timers fire only when a
// test advances it.
type fakeClock struct{ *clocktest.Fake }

func newFakeClock() *fakeClock {
	return &fakeClock{clocktest.New(time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))}
}

// advance moves the clock forward by d, firing each due timer in time
// order and letting the loop process what it posted before the next.
func (c *fakeClock) advance(n *Names, d time.Duration) {
	target := c.Now().Add(d)
	for {
		at, ok := c.Next()
		if !ok || at.After(target) {
			break
		}
		c.Advance(max(at.Sub(c.Now()), 0))
		_ = n.loop.call(func() {})
	}
	c.Advance(max(target.Sub(c.Now()), 0))
	_ = n.loop.call(func() {})
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

func ptrRR(name, target string, ttl uint32) dns.RR {
	return &dns.PTR{Hdr: hdr(name, dns.TypePTR, ttl, false), Ptr: dns.Fqdn(target)}
}

// inject queues a response on the fake connection, as the network would.
func inject(t *testing.T, c *fakeConn, ifIndex int, answers []dns.RR, extra ...dns.RR) {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Answer = answers
	m.Extra = extra
	b, err := m.Pack()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	c.out <- mdns.QueryResponse{Msg: b, IfIndex: ifIndex}
}

// asked reports whether any sent query asks for name/type.
func asked(t *testing.T, c *fakeConn, name string, typ mdns.DNSType) bool {
	t.Helper()
	for _, q := range c.queries(t) {
		for _, qq := range q.Questions {
			if qq.Name == name && qq.Type == typ {
				return true
			}
		}
	}
	return false
}
