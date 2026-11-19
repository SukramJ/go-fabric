// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport"
	"github.com/SukramJ/go-fabric/transport/tcp"
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
// a TCP connection the chunker may fill the connection's frame, bounded by
// the peer's stated MaxTcpMessageSize, 64000 when it states none, never
// below 1232 — matter.js MessageChannel.maxPayloadSize and
// Session.peerMaxTcpMessageSize (MessageChannel.ts:86-92, Session.ts:237-239
// at db021da0).
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
	if got := b.chunkLimitsFor(route, 1); got != (chunkLimits{budget: 1232 - 200, hardCap: 1232 - 200}) {
		t.Fatalf("a peer bound below 1232 is raised to it: %+v", got)
	}
	b.mu.Lock()
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 1500}
	b.mu.Unlock()
	if got := b.chunkLimitsFor(route, 1); got != (chunkLimits{budget: reportChunkPayloadBudget, hardCap: 1500 - 200}) {
		t.Fatalf("peer bound 1500: %+v, want the UDP budget under a 1300 hard cap", got)
	}
	// A connection larger than 64000: a peer that states nothing is
	// bounded by the 64000 fallback.
	big := b.routeFor(&sizedConn{size: 100_000})
	b.mu.Lock()
	b.sessions = prev
	b.mu.Unlock()
	if got := b.chunkLimitsFor(big, 1); got.budget != 64_000-200 || got.hardCap != 64_000-200 {
		t.Fatalf("no peer bound on a 100000-byte connection: %+v, want the 64000 fallback", got)
	}
}

// TestTCPSendAboveThePeerLimitIsRefused: over TCP a message above the
// route's limit for its session — the peer's stated MaxTcpMessageSize
// here — is refused before the write and never reaches the connection;
// one within it is sent; over UDP the check does not apply. matter.js
// MessageChannel.send throws MessageTooLargeError on a TCP channel before
// the write (packages/protocol/src/protocol/MessageChannel.ts:104-112 at
// b25ed960).
func TestTCPSendAboveThePeerLimitIsRefused(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	conn := &sizedConn{size: 64_000}
	route := b.routeFor(conn)
	b.mu.Lock()
	prev := b.sessions
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 4000}
	b.mu.Unlock()
	if err := b.sendRouted(nil, route, 1, make([]byte, 4001)); !errors.Is(err, tcp.ErrMessageTooLarge) {
		t.Fatalf("4001 bytes on a 4000-byte peer: %v, want tcp.ErrMessageTooLarge", err)
	}
	if err := b.sendRouted(nil, route, 1, make([]byte, 4000)); err != nil {
		t.Fatalf("4000 bytes on a 4000-byte peer: %v", err)
	}
	// Session 2 states nothing: 64000.
	if err := b.sendRouted(nil, route, 2, make([]byte, 8000)); err != nil {
		t.Fatalf("8000 bytes without a peer bound: %v", err)
	}
	conn.mu.Lock()
	sent := len(conn.sent)
	conn.mu.Unlock()
	if sent != 2 {
		t.Fatalf("%d messages reached the connection, want 2 (the refused one not)", sent)
	}
}

// TestReportChunksStayWithinThePeerLimit: the chunks of a report to a TCP
// peer that states a small MaxTcpMessageSize each encode within the limit
// the route gives the chunker.
func TestReportChunksStayWithinThePeerLimit(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	route := b.routeFor(&sizedConn{size: 64_000})
	b.mu.Lock()
	prev := b.sessions
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 3000}
	b.mu.Unlock()
	limits := b.chunkLimitsFor(route, 1)
	if limits.hardCap != 3000-200 {
		t.Fatalf("limits %+v, want a 2800 hard cap", limits)
	}
	reports := make([]im.AttributeReport, 400)
	for i := range reports {
		reports[i] = makeAttributeReport(uint16(i + 1)) //nolint:gosec // bounded fixture
	}
	chunks, err := chunkReportData(im.ReportData{Reports: reports}, limits.budget, limits.hardCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("%d chunks, want the report split", len(chunks))
	}
	for i, c := range chunks {
		enc, err := EncodeReportData(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(enc) > limits.budget {
			t.Fatalf("chunk %d encodes to %d bytes, above the %d budget", i, len(enc), limits.budget)
		}
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
