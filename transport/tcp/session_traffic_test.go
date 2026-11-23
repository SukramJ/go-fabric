// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

// TestSessionTrafficIsTheIdleMeasureOnceNoted: once the host noted session
// traffic on a connection, the listener measures the connection's idleness
// by those notes alone, so a peer that keeps a session-carrying
// connection's place by sending any frame now and then loses it once its
// sessions are idle past idleGrace; a connection with no note keeps the
// measure of every frame (review II, N3-finnet-4).
func TestSessionTrafficIsTheIdleMeasureOnceNoted(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	handler := func(_ *Conn, _ []byte, release func()) { release() }
	held := make([]*Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		c, _ := acceptFrom(t, l, i, handler)
		if c == nil {
			t.Fatalf("connection %d was not held", i+1)
		}
		c.received.Store(true) // past the new connection's idle timeout
		held = append(held, c)
	}
	keeper := held[0]
	keeper.NoteSessionTraffic()
	for range 6 { // six minutes of frames that carry no session's message
		fake.Advance(time.Minute)
		for _, c := range held {
			c.markTraffic()
		}
	}
	newcomer, _ := acceptFrom(t, l, 10, handler)
	if newcomer == nil {
		t.Fatal("the new connection was refused although one connection's sessions were idle past idleGrace")
	}
	select {
	case <-keeper.Closed():
	default:
		t.Fatal("the connection whose sessions were idle kept its place by its frames")
	}
	for i, c := range held[1:] {
		select {
		case <-c.Closed():
			t.Errorf("connection %d, never noted, lost its place although its frames are recent", i+2)
		default:
		}
	}
}

// acceptFromIP is acceptFrom with the source address chosen by its last
// group, so several connections can come from one address.
func acceptFromIP(t *testing.T, l *Listener, ipGroup, port int, handler Handler) *Conn {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { _ = cli.Close() })
	before := l.connSet()
	l.accept(pipeFrom{Conn: srv, remote: &net.TCPAddr{IP: net.ParseIP(fmt.Sprintf("fd00::%x", ipGroup)), Port: port}}, handler, nil)
	for c := range l.connSet() {
		if !before[c] {
			return c
		}
	}
	return nil
}

// TestSessionlessConnectionsHoldNoPlaceByTheirFrames: two source addresses
// that never establish a session hold three of the four places with a
// frame now and then, beside a connection that carries a session. Their
// frames earn them no place: past sessionlessGrace from its accept a
// connection that carried no session is reclaimable, whatever it sent, so
// a newcomer takes the oldest of their places and the session-carrying
// connection keeps its own (review II, N3-finnet-4, round 2).
func TestSessionlessConnectionsHoldNoPlaceByTheirFrames(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	handler := func(_ *Conn, _ []byte, release func()) { release() }
	session := acceptFromIP(t, l, 0x10, 40000, handler)
	sessionless := []*Conn{
		acceptFromIP(t, l, 0x20, 40001, handler),
		acceptFromIP(t, l, 0x20, 40002, handler),
		acceptFromIP(t, l, 0x30, 40003, handler),
	}
	for i, c := range append([]*Conn{session}, sessionless...) {
		if c == nil {
			t.Fatalf("connection %d was not held", i+1)
		}
		c.received.Store(true) // past the new connection's idle timeout
	}
	for range 3 { // three minutes: a frame on every connection each minute
		fake.Advance(time.Minute)
		session.NoteSessionTraffic()
		for _, c := range append([]*Conn{session}, sessionless...) {
			c.markTraffic()
		}
	}
	if newcomer := acceptFromIP(t, l, 0x40, 40004, handler); newcomer == nil {
		t.Fatal("the newcomer was refused although three connections never carried a session")
	}
	select {
	case <-session.Closed():
		t.Fatal("the session-carrying connection lost its place")
	default:
	}
	select {
	case <-sessionless[0].Closed():
	default:
		t.Fatal("the oldest sessionless connection kept its place by its frames")
	}
}

// TestSentMessagesOfANotedConnectionKeepItsPlace: once the host noted
// session traffic on a connection, a message the device sends on it counts
// as session traffic too — a connection whose sessions only send (a
// subscription's reports) keeps its place at the total bound, and the one
// quiet for idleGrace is reclaimed. A connection never noted earns nothing
// by sending. R2-C2-bound-0201.
func TestSentMessagesOfANotedConnectionKeepItsPlace(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	handler := func(_ *Conn, _ []byte, release func()) { release() }
	held := make([]*Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		c, cli := acceptFrom(t, l, i, handler)
		go func() { _, _ = io.Copy(io.Discard, cli) }()
		c.received.Store(true)
		c.NoteSessionTraffic()
		held = append(held, c)
	}
	sender, quiet := held[0], held[MaxInboundConnections-1]
	for range 6 {
		fake.Advance(time.Minute)
		if err := sender.Send([]byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		for _, c := range held[1 : MaxInboundConnections-1] {
			c.NoteSessionTraffic()
		}
	}
	acceptFrom(t, l, 10, handler)
	select {
	case <-sender.Closed():
		t.Fatal("the connection whose session sent every minute was reclaimed")
	default:
	}
	select {
	case <-quiet.Closed():
	default:
		t.Fatal("the connection quiet for idleGrace kept its place")
	}
}

// TestSentMessagesAfterTheLastSessionEndedEarnNoPlace: once the host noted
// that a connection's last session ended (a handshake still in flight on
// it), what the device sends there — replies to a peer's unsecured
// handshake frames — no longer renews the connection's place; it is
// reclaimed at the bound idleGrace after its last session traffic. A new
// note of session traffic restores the renewal. R3-bound-3402.
func TestSentMessagesAfterTheLastSessionEndedEarnNoPlace(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	handler := func(_ *Conn, _ []byte, release func()) { release() }
	held := make([]*Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		c, cli := acceptFrom(t, l, i, handler)
		go func() { _, _ = io.Copy(io.Discard, cli) }()
		c.received.Store(true)
		c.NoteSessionTraffic()
		held = append(held, c)
	}
	ended, renewed := held[0], held[1]
	ended.NoteSessionsEnded()
	renewed.NoteSessionsEnded()
	renewed.NoteSessionTraffic() // a new session on it
	for range 6 {
		fake.Advance(time.Minute)
		for _, c := range []*Conn{ended, renewed} {
			if err := c.Send([]byte{1, 2, 3}); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range held[2:] {
			c.NoteSessionTraffic()
		}
	}
	acceptFrom(t, l, 10, handler)
	select {
	case <-ended.Closed():
	default:
		t.Fatal("the connection whose last session ended kept its place by the replies it sent")
	}
	select {
	case <-renewed.Closed():
		t.Fatal("the connection noted again after its sessions ended lost its place although it sent every minute")
	default:
	}
}
