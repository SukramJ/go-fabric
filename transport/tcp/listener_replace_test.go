// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

// At its total bound the listener gives the place of the connection that
// has been idle longest to a new one, sparing every connection that
// carried traffic within the session's idle timeout (review II,
// R2-P3-dr-net-02).

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestIdleGraceIsTheSessionIdleTimeout: a connection is spared for as
// long as a secure session on it is — the operational session manager's
// idle timeout.
func TestIdleGraceIsTheSessionIdleTimeout(t *testing.T) {
	t.Parallel()
	if idleGrace != operational.SessionIdleTimeout {
		t.Fatalf("idleGrace = %v, operational.SessionIdleTimeout = %v", idleGrace, operational.SessionIdleTimeout)
	}
}

// TestListenerGivesTheIdleLongestPlaceToANewConnection: four connections
// from two addresses that each delivered one message hold the listener's
// places only while they carry traffic. Once each has been idle past the
// grace, a new connection takes the place of the one idle longest; a
// connection that received or sent a message within the grace keeps its
// place; when every held one has, the new one is closed.
func TestListenerGivesTheIdleLongestPlaceToANewConnection(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	got := make(chan struct{}, 16)
	handler := func(_ *Conn, _ []byte, release func()) { release(); got <- struct{}{} }

	held := make([]*Conn, 0, MaxInboundConnections)
	clients := make([]net.Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		addr := 0 // three from one address, the fourth from a second
		if i == MaxConnectionsPerPeerIP {
			addr = 1
		}
		c, cli := acceptFrom(t, l, addr, handler)
		if c == nil {
			t.Fatalf("connection %d of %d was not held", i+1, MaxInboundConnections)
		}
		if _, err := cli.Write(frameOf([]byte{1})); err != nil {
			t.Fatal(err)
		}
		<-got
		held, clients = append(held, c), append(clients, cli)
		fake.Advance(time.Second) // a distinct last-traffic time each
	}

	// Within the grace every held one carried traffic: a new one is closed.
	if c, _ := acceptFrom(t, l, 2, handler); c != nil {
		t.Fatal("a new connection took the place of one that carried traffic within the grace")
	}

	fake.Advance(idleGrace)
	// The oldest receives a message again, the third sends one: both are
	// spared.
	if _, err := clients[0].Write(frameOf([]byte{2})); err != nil {
		t.Fatal(err)
	}
	<-got
	go func() { _, _ = io.Copy(io.Discard, clients[2]) }()
	if err := held[2].Send([]byte{3}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []int{1, 3} {
		c, _ := acceptFrom(t, l, 3+want, handler)
		if c == nil {
			t.Fatalf("a new connection was refused although held connection %d had been idle past the grace (%v)", want, idleGrace)
		}
		select {
		case <-held[want].Closed():
			if !errors.Is(held[want].Err(), errReclaimed) {
				t.Errorf("held connection %d closed with %v, want errReclaimed", want, held[want].Err())
			}
		default:
			t.Fatalf("held connection %d, idle longest, is still open", want)
		}
		if n := l.Conns(); n != MaxInboundConnections {
			t.Fatalf("conns = %d, want %d", n, MaxInboundConnections)
		}
	}
	for _, spared := range []int{0, 2} {
		select {
		case <-held[spared].Closed():
			t.Errorf("held connection %d, with traffic within the grace, closed: %v", spared, held[spared].Err())
		default:
		}
	}
}
