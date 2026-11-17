// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestListenerSparesAFreshConnectionFromReclaim holds R2-P6-bound-0203: a
// connection that has not yet carried its first message keeps its place
// for reclaimAgeFloor after its accept, so silent connections from other
// source addresses cannot take its place before its first message
// (PBKDFParamRequest, Sigma1) arrives. Past the floor a silent one is
// reclaimable as before.
func TestListenerSparesAFreshConnectionFromReclaim(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64, Clock: fake})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	handler := func(_ *Conn, _ []byte, release func()) { release() }

	silent := make([]*Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		c, _ := acceptFrom(t, l, i, handler)
		if c == nil {
			t.Fatalf("silent connection %d was not held", i+1)
		}
		silent = append(silent, c)
	}
	// Within the floor the silent ones are spared: a new one is refused.
	if c, _ := acceptFrom(t, l, 10, handler); c != nil {
		t.Fatal("a new connection took the place of one younger than the floor")
	}
	fake.Advance(reclaimAgeFloor)
	fresh, _ := acceptFrom(t, l, 11, handler)
	if fresh == nil {
		t.Fatal("the fresh connection was refused although silent ones were past the floor")
	}
	// Silent connections from new addresses arrive before its first message.
	for i := range MaxInboundConnections {
		_, _ = acceptFrom(t, l, 20+i, handler)
	}
	select {
	case <-fresh.Closed():
		t.Fatalf("the fresh connection was reclaimed before its first message: %v", fresh.Err())
	default:
	}
	for i, c := range silent {
		select {
		case <-c.Closed():
		default:
			t.Errorf("silent connection %d, past the floor, still holds a place", i+1)
		}
	}
	if n := l.Conns(); n != MaxInboundConnections {
		t.Fatalf("conns = %d, want %d", n, MaxInboundConnections)
	}
}
