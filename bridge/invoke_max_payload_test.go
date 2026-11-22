// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport"
	"thingfabric.dev/go-fabric/transport/tcp"
)

// payloadBoundServer records the exchange bound its command ran with.
type payloadBoundServer struct {
	countingInvokeServer
	bound atomic.Int64 // -1 when the context carried none
}

func (s *payloadBoundServer) MatterInvoke(ctx context.Context, cmd uint32, fields any) (any, error) {
	if n, ok := im.MaxPayloadSizeFromContext(ctx); ok {
		s.bound.Store(int64(n))
	} else {
		s.bound.Store(-1)
	}
	return s.countingInvokeServer.MatterInvoke(ctx, cmd, fields)
}

// TestInvokeCarriesTheExchangeMaxPayload: a command runs with the largest
// payload one message of its exchange carries, matter.js
// MessageExchange.maxPayloadSize (packages/protocol/src/protocol/MessageExchange.ts:465-469
// at db021da0): over UDP 1232 - 54 = 1178, over TCP the connection's
// largest message less 54.
func TestInvokeCarriesTheExchangeMaxPayload(t *testing.T) {
	t.Parallel()
	path := im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}

	t.Run("UDP", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		srv := &payloadBoundServer{countingInvokeServer: countingInvokeServer{id: 0x0006}}
		b.AttachRootClusters([]contract.ClusterServer{srv})
		peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = peerConn.Close() })
		proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeInvokeRequest)
		if err := b.dispatch(context.Background(), secureTestDatagram(t, b, proto, encodeTimedTestInvokeRequest(t, false, path)), peerConn.LocalAddr()); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if got := srv.bound.Load(); got != 1178 {
			t.Errorf("bound %d, want 1178", got)
		}
	})

	t.Run("TCP", func(t *testing.T) {
		t.Parallel()
		h := newTCPHarness(t)
		srv := &payloadBoundServer{countingInvokeServer: countingInvokeServer{id: 0x0006}}
		h.bridge.AttachRootClusters([]contract.ClusterServer{srv})
		conn, replies := h.dial()
		if err := conn.Send(h.sealedInvoke(0x340)); err != nil {
			t.Fatal(err)
		}
		waitReply(t, replies)
		if got, want := srv.bound.Load(), int64(tcp.DefaultMaxPayloadSize-54); got != want {
			t.Errorf("bound %d, want %d", got, want)
		}
	})
}

// TestInvokeMaxPayloadSizeFollowsThePeerBound: over a connection the bound
// is the smaller of the connection's frame and the MaxTcpMessageSize the
// session's peer stated, less the 54-byte message overhead (matter.js
// MessageChannel.ts:86-92, MessageExchange.ts:465-469 at db021da0), never
// below the UDP bound.
func TestInvokeMaxPayloadSizeFollowsThePeerBound(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	if got := b.invokeMaxPayloadSize(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, 1); got != 1178 {
		t.Fatalf("UDP route: %d, want 1178", got)
	}
	var c transport.Conn = &sizedConn{size: 64_000}
	route := b.routeFor(c)
	if got := b.invokeMaxPayloadSize(route, 1); got != 64_000-54 {
		t.Fatalf("connection without a peer bound: %d", got)
	}
	b.mu.Lock()
	prev := b.sessions
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 8192}
	b.mu.Unlock()
	if got := b.invokeMaxPayloadSize(route, 1); got != 8192-54 {
		t.Fatalf("peer bound 8192: %d", got)
	}
	b.mu.Lock()
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 1000}
	b.mu.Unlock()
	if got := b.invokeMaxPayloadSize(route, 1); got != 1178 {
		t.Fatalf("a peer bound below the UDP bound: %d, want 1178", got)
	}
}

// TestInitiatedExchangeMaxPayloadSizeFollowsThePeerBound: an exchange over
// a connection carries at most the smaller of the connection's frame and
// the MaxTcpMessageSize the session's peer stated, less the 54-byte
// message overhead, never below the UDP bound — the bound a reply on the
// same session keeps ([Bridge.chunkLimitsFor]). matter.js
// MessageChannel.maxPayloadSize caps a TCP channel at
// Session.peerMaxTcpMessageSize
// (packages/protocol/src/protocol/MessageChannel.ts:86-92,
// packages/protocol/src/session/Session.ts:237-239 at db021da0), and
// MessageExchange.maxPayloadSize subtracts the overhead
// (MessageExchange.ts:465-469).
func TestInitiatedExchangeMaxPayloadSizeFollowsThePeerBound(t *testing.T) {
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	if got := (&initiatedSecureExchange{b: b, sessionID: 1, addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}}).MaxPayloadSize(); got != 1178 {
		t.Fatalf("UDP route: %d, want 1178", got)
	}
	var c transport.Conn = &sizedConn{size: 64_000}
	ex := &initiatedSecureExchange{b: b, sessionID: 1, addr: b.routeFor(c)}
	if got := ex.MaxPayloadSize(); got != 64_000-54 {
		t.Fatalf("connection without a peer bound: %d, want %d", got, 64_000-54)
	}
	b.mu.Lock()
	prev := b.sessions
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 8192}
	b.mu.Unlock()
	if got := ex.MaxPayloadSize(); got != 8192-54 {
		t.Fatalf("peer bound 8192: %d, want %d", got, 8192-54)
	}
	b.mu.Lock()
	b.sessions = stubTransports{SessionLookup: prev, sid: 1, trans: 1 << 2, max: 1000}
	b.mu.Unlock()
	if got := ex.MaxPayloadSize(); got != 1178 {
		t.Fatalf("a peer bound below the UDP bound: %d, want 1178", got)
	}
}
