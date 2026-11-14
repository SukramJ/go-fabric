// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// queueConn is an mdns.QueryConn that sends nothing and delivers what a
// test queues.
type queueConn struct{ out chan mdns.QueryResponse }

func (q *queueConn) Send([]byte) error                    { return nil }
func (q *queueConn) Responses() <-chan mdns.QueryResponse { return q.out }
func (q *queueConn) Close() error                         { return nil }

// stackHolds reports whether one goroutine's stack holds every frame.
func stackHolds(frames ...string) bool {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		all := true
		for _, f := range frames {
			if !strings.Contains(g, f) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// TestDiagnoseMDNSHoldsNoGateAcrossTheCache (review II, R2-P2-dr-ctl-01):
// DiagnoseMDNS waits for the DNS-SD cache's goroutine without holding the
// closed gate. The cache goroutine runs an observer of a discovery event
// that reads DiagnosticEvents, as OnDiagnosticEvent allows; DiagnoseMDNS
// waits for that goroutine and a Close waits for the gate's write lock.
// Before, DiagnoseMDNS held the gate's read lock across the wait, the
// observer's read lock queued behind the waiting Close, and none of the
// three returned.
func TestDiagnoseMDNSHoldsNoGateAcrossTheCache(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	conn := &queueConn{out: make(chan mdns.QueryResponse, 4)}
	cfid := [8]byte{0x87, 0xE1, 0xB0, 0x04, 0xE2, 0x35, 0xA1, 0x30}
	c := &Controller{
		diagRing: diagevent.NewRing(diag.DefaultDiagnosticsCapacity),
		metrics:  newFakeMetrics(),
		names:    discovery.NewNames(conn, slog.New(slog.DiscardHandler)),
		fabric:   &commissioner.Fabric{Fabric: handshake.Fabric{Identity: &sigma.Identity{CompressedFabricID: cfid}}},
		stop:     func() {},
	}
	c.registry.Store(&client.Registry{})
	c.stopDiscover = c.watchDiscovery()

	entered, release, observed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	c.OnDiagnosticEvent(func(e diag.Event) {
		if e.Kind != diagevent.KindDiscovery {
			return
		}
		once.Do(func() {
			close(entered)
			<-release
			_ = c.DiagnosticEvents()
			close(observed)
		})
	})

	// An operational record of the fabric's node 1: the watch records it
	// on the cache goroutine.
	host := "0011223344556677.local."
	instance := "87E1B004E235A130-0000000000000001._matter._tcp.local."
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.SRV{Hdr: dns.RR_Header{Name: instance, Rrtype: dns.TypeSRV, Class: dns.ClassINET | 0x8000, Ttl: 120}, Target: host, Port: 5540},
		&dns.AAAA{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeAAAA, Class: dns.ClassINET | 0x8000, Ttl: 120}, AAAA: net.ParseIP("fe80::1")},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn.out <- mdns.QueryResponse{Msg: b, IfIndex: 1}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the discovery event did not reach the observer")
	}

	// DiagnoseMDNS, until it waits for the cache goroutine.
	diagnosed := make(chan struct{})
	go func() {
		defer close(diagnosed)
		_ = c.DiagnoseMDNS()
	}()
	for !stackHolds("(*Controller).DiagnoseMDNS", "(*loop).call") {
		if ctx.Err() != nil {
			t.Fatal("DiagnoseMDNS never waited for the cache goroutine")
		}
		runtime.Gosched()
	}

	// Close, until it waits for the gate's write lock or holds it.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = c.Close(ctx)
	}()
	for c.closeMu.TryRLock() { // false: a writer waits for or holds the gate
		began := c.closed
		c.closeMu.RUnlock()
		if began {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("Close never reached the gate")
		}
		runtime.Gosched()
	}

	close(release)
	for what, done := range map[string]chan struct{}{"DiagnoseMDNS": diagnosed, "Close": closed, "the observer": observed} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("%s did not return: the closed gate deadlocked", what)
		}
	}
}
