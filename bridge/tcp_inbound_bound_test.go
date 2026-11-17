// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// The device's bound on inbound TCP connections: TCPConfig's
// MaxInboundConnections, tcp.MaxInboundConnections by default (review II,
// owner decision 56).

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// TestTCPInboundBoundDefaultsToTheDeviceBound: the zero TCPConfig bounds
// the listener at tcp.MaxInboundConnections; a configured value is kept.
func TestTCPInboundBoundDefaultsToTheDeviceBound(t *testing.T) {
	t.Parallel()
	if got := (TCPConfig{Incoming: true}).maxInboundConnections(); got != tcp.MaxInboundConnections {
		t.Errorf("default bound %d, want tcp.MaxInboundConnections (%d)", got, tcp.MaxInboundConnections)
	}
	if got := (TCPConfig{Incoming: true, MaxInboundConnections: 9}).maxInboundConnections(); got != 9 {
		t.Errorf("configured bound 9 resolves to %d", got)
	}
}

// TestTCPListenerHoldsTheConfiguredInboundBound: a bridge whose TCP
// listener is bounded at one connection gives a second connection the
// place of the first, which has delivered no message and is past the age
// that spares a fresh connection (a fraction of
// tcp.NewConnectionIdleTimeout, review II R2-P6-bound-0203).
func TestTCPListenerHoldsTheConfiguredInboundBound(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	h := newConnHarness(t, Config{
		Listen: "127.0.0.1:0",
		TCP:    TCPConfig{Incoming: true, MaxInboundConnections: 1},
		Clock:  fake,
	})
	first, _ := h.dial()
	h.bridge.mu.RLock()
	tl := h.bridge.tcpListener
	h.bridge.mu.RUnlock()
	waitFor(t, "the first connection held", func() bool { return tl.Conns() == 1 })
	fake.Advance(tcp.NewConnectionIdleTimeout / 2)
	second, _ := h.dial()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-first.Closed():
	case <-ctx.Done():
		t.Fatal("the first connection is still open beside a second under a bound of one")
	}
	select {
	case <-second.Closed():
		t.Fatalf("the second connection closed: %v", second.Err())
	default:
	}
}

// TestNewRefusesANegativeTCPInboundBound: a negative
// TCPConfig.MaxInboundConnections is refused by New.
func TestNewRefusesANegativeTCPInboundBound(t *testing.T) {
	t.Parallel()
	if _, err := New(wbEmptySnapshotter, newFakeAdvertiser(), Config{
		Listen: "127.0.0.1:0",
		TCP:    TCPConfig{Incoming: true, MaxInboundConnections: -1},
	}, nil); err == nil {
		t.Fatal("New with TCP.MaxInboundConnections -1 succeeded")
	}
}
