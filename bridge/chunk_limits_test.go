// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/transport"
)

// sizedConn is a reliable connection with a frame size, as *tcp.Conn.
type sizedConn struct {
	fakeConn
	size int
}

func (c *sizedConn) MaxMessageSize() int { return c.size }

// stubTransports answers TransportParametersFor for one session.
type stubTransports struct {
	SessionLookup
	sid   uint16
	trans uint16
	max   uint32
}

func (s stubTransports) TransportParametersFor(sessionID uint16) (transports uint16, maxTCPMessageSize uint32, ok bool) {
	if sessionID != s.sid {
		return 0, 0, false
	}
	return s.trans, s.max, true
}

// TestChunkLimitsForRoute: replies over UDP keep the datagram budget; over
// a reliable connection the chunker may fill the connection's frame,
// bounded by the peer's stated MaxTcpMessageSize (matter.js keeps it on
// the Session, Session.ts:86, :118; TcpChannel.ts:125-126 refuses a larger
// message), never below the UDP limits.
func TestChunkLimitsForRoute(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	udp := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	if got := b.chunkLimitsFor(udp, 1); got != udpChunkLimits {
		t.Fatalf("udp route: %+v", got)
	}
	conn := &sizedConn{size: 64_000 - 4}
	var c transport.Conn = conn
	route := b.routeFor(c)
	if got := b.chunkLimitsFor(route, 1); got.budget != 64_000-4-200 || got.hardCap != got.budget {
		t.Fatalf("tcp route without a peer bound: %+v", got)
	}
	b.mu.Lock()
	prev := b.sessions
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 8192}
	b.mu.Unlock()
	if got := b.chunkLimitsFor(route, 1); got.budget != 8192-200 {
		t.Fatalf("peer bound 8192: %+v", got)
	}
	if got := b.chunkLimitsFor(route, 2); got.budget != 64_000-4-200 {
		t.Fatalf("unknown session keeps the frame size: %+v", got)
	}
	b.mu.Lock()
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 1000}
	b.mu.Unlock()
	if got := b.chunkLimitsFor(route, 1); got != udpChunkLimits {
		t.Fatalf("a peer bound below the UDP limits falls back to them: %+v", got)
	}
}

// TestEncodesAttributeValue: the exported predicate says whether the
// writer knows a value's type; nil and an unknown struct encode as null.
func TestEncodesAttributeValue(t *testing.T) {
	t.Parallel()
	if !EncodesAttributeValue(uint16(7)) || !EncodesAttributeValue("x") || !EncodesAttributeValue([]uint16{1}) {
		t.Fatal("known types reported as not encodable")
	}
	type stranger struct{ A int }
	if EncodesAttributeValue(stranger{1}) || EncodesAttributeValue(nil) {
		t.Fatal("unknown type or nil reported as encodable")
	}
}
