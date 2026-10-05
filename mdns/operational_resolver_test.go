// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeQueryConn answers queries through a scripted responder.
type fakeQueryConn struct {
	mu      sync.Mutex
	queries []*dns.Msg
	out     chan received
	respond func(q *dns.Msg, n int) []received
}

func newFakeQueryConn(respond func(q *dns.Msg, n int) []received) *fakeQueryConn {
	return &fakeQueryConn{out: make(chan received, 64), respond: respond}
}

func (f *fakeQueryConn) Send(msg []byte) error {
	q := new(dns.Msg)
	if err := q.Unpack(msg); err != nil {
		return err
	}
	f.mu.Lock()
	f.queries = append(f.queries, q)
	n := len(f.queries)
	f.mu.Unlock()
	for _, r := range f.respond(q, n) {
		f.out <- r
	}
	return nil
}

func (f *fakeQueryConn) Responses() <-chan received { return f.out }
func (f *fakeQueryConn) Close() error               { return nil }

func (f *fakeQueryConn) sent() []*dns.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*dns.Msg(nil), f.queries...)
}

func resolverOver(c *fakeQueryConn) *OperationalResolver {
	return &OperationalResolver{logger: nil, open: func() (queryConn, error) { return c, nil }}
}

func packResponse(t *testing.T, records ...dns.RR) []byte {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Authoritative = true
	m.Answer = records
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	testCFID   = [8]byte{0x87, 0xE1, 0xB0, 0x04, 0xE2, 0x35, 0xA1, 0x30}
	testNodeID = uint64(0x8FC7772401CD0696)
)

func srvRR(instance, target string, port uint16) dns.RR {
	return &dns.SRV{Hdr: dns.RR_Header{Name: instance, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120}, Target: target, Port: port}
}

func aaaaRR(host, ip string) dns.RR {
	return &dns.AAAA{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120}, AAAA: net.ParseIP(ip)}
}

func aRR(host, ip string) dns.RR {
	return &dns.A{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120}, A: net.ParseIP(ip)}
}

func TestOperationalResolver_ResolvesFromOneResponseInPreferenceOrder(t *testing.T) {
	t.Parallel()
	instance := OperationalInstanceQName(testCFID, testNodeID)
	conn := newFakeQueryConn(func(q *dns.Msg, _ int) []received {
		// Noise first: a foreign query and a goodbye for our instance.
		noise := new(dns.Msg)
		noise.SetQuestion("other.local.", dns.TypePTR)
		noiseBytes, _ := noise.Pack()
		goodbye := srvRR(instance, "stale.local.", 1)
		goodbye.Header().Ttl = 0
		return []received{
			{msg: noiseBytes},
			{msg: packResponse(t, goodbye)},
			{msg: packResponse(
				t,
				aRR("ctrl.local.", "192.168.1.20"),
				aaaaRR("ctrl.local.", "2001:db8::20"),
				aaaaRR("ctrl.local.", "fd00::20"),
				srvRR(instance, "ctrl.local.", 5540),
			)},
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	addrs, err := resolverOver(conn).ResolveOperational(ctx, testCFID, testNodeID)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(addrs))
	for _, a := range addrs {
		got = append(got, a.String())
	}
	want := "[fd00::20]:5540 [2001:db8::20]:5540 192.168.1.20:5540"
	if strings.Join(got, " ") != want {
		t.Fatalf("addresses = %v, want %s", got, want)
	}
	q := conn.sent()[0]
	if len(q.Question) != 2 || q.Question[0].Qtype != dns.TypeSRV || q.Question[1].Qtype != dns.TypeTXT ||
		!strings.EqualFold(q.Question[0].Name, instance) {
		t.Fatalf("first query = %v, want SRV+TXT for %s", q.Question, instance)
	}
}

// TestOperationalResolver_AsksForHostAddressesAfterBareSRV mirrors
// IpServiceResolution: an SRV target without addresses triggers an A/AAAA
// query for it at once.
func TestOperationalResolver_AsksForHostAddressesAfterBareSRV(t *testing.T) {
	t.Parallel()
	instance := OperationalInstanceQName(testCFID, testNodeID)
	conn := newFakeQueryConn(func(q *dns.Msg, n int) []received {
		if n == 1 {
			return []received{{msg: packResponse(t, srvRR(instance, "ctrl.local.", 5540))}}
		}
		for _, qq := range q.Question {
			if qq.Qtype == dns.TypeAAAA && strings.EqualFold(qq.Name, "ctrl.local.") {
				return []received{{msg: packResponse(t, aaaaRR("CTRL.local.", "fd00::1"))}}
			}
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	addrs, err := resolverOver(conn).ResolveOperational(ctx, testCFID, testNodeID)
	if err != nil {
		t.Fatalf("resolve: %v (queries %d)", err, len(conn.sent()))
	}
	if len(addrs) != 1 || addrs[0].String() != "[fd00::1]:5540" {
		t.Fatalf("addresses = %v", addrs)
	}
}

func TestOperationalResolver_RetriesThenGivesUp(t *testing.T) {
	t.Parallel()
	conn := newFakeQueryConn(func(*dns.Msg, int) []received { return nil })
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	_, err := resolverOver(conn).ResolveOperational(ctx, testCFID, testNodeID)
	if !errors.Is(err, ErrOperationalNotResolved) {
		t.Fatalf("err = %v, want ErrOperationalNotResolved", err)
	}
	// Query at 0 and one retry after 1 s ± 20 %.
	if n := len(conn.sent()); n != 2 {
		t.Fatalf("queries sent = %d, want 2 within 1.5 s", n)
	}
}

func TestOperationalResolver_DropsLinkLocalWithoutZone(t *testing.T) {
	t.Parallel()
	instance := OperationalInstanceQName(testCFID, testNodeID)
	conn := newFakeQueryConn(func(*dns.Msg, int) []received {
		return []received{{msg: packResponse(
			t,
			srvRR(instance, "ctrl.local.", 5540),
			aaaaRR("ctrl.local.", "fe80::1"),
			aRR("ctrl.local.", "10.0.0.2"),
		)}}
	})
	addrs, err := resolverOver(conn).ResolveOperational(t.Context(), testCFID, testNodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0].String() != "10.0.0.2:5540" {
		t.Fatalf("addresses = %v, want only the routable IPv4 one", addrs)
	}
}
