// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
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
