// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// largeMessageFactServer records the Large Message fact its command ran
// with: 1 true, 0 false, -1 unstated.
type largeMessageFactServer struct {
	countingInvokeServer
	fact atomic.Int32
}

func (s *largeMessageFactServer) MatterInvoke(ctx context.Context, cmd uint32, fields any) (any, error) {
	switch large, known := im.LargeMessageSessionFromContext(ctx); {
	case !known:
		s.fact.Store(-1)
	case large:
		s.fact.Store(1)
	default:
		s.fact.Store(0)
	}
	return s.countingInvokeServer.MatterInvoke(ctx, cmd, fields)
}

// bleLikeConn is a reliable connection that is not TCP, as a BTP one.
type bleLikeConn struct{ fakeConn }

type bleAddr struct{}

func (bleAddr) Network() string { return "ble" }
func (bleAddr) String() string  { return "ble" }

func (*bleLikeConn) Remote() net.Addr { return bleAddr{} }

// TestInvokeStatesWhetherTheSessionCarriesLargeMessages: the bridge states
// the session fact the invoke path's Large Message gate reads
// ([im.WithLargeMessageSession]) — false over UDP, true over a TCP
// connection — so a Large Message command on a UDP session answers
// InvalidTransportType (review II, R2-P4-imwrite-01; matter.js
// CommandInvokeResponse.ts:281-285 at db021da0, connectedhomeip
// SecureSession.h:163 at 6170af84).
func TestInvokeStatesWhetherTheSessionCarriesLargeMessages(t *testing.T) {
	t.Parallel()
	path := im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}

	t.Run("UDP", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		srv := &largeMessageFactServer{countingInvokeServer: countingInvokeServer{id: 0x0006}}
		srv.fact.Store(-2)
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
		if got := srv.fact.Load(); got != 0 {
			t.Errorf("fact %d, want 0 (stated: no large messages)", got)
		}
	})

	t.Run("TCP", func(t *testing.T) {
		t.Parallel()
		h := newTCPHarness(t)
		srv := &largeMessageFactServer{countingInvokeServer: countingInvokeServer{id: 0x0006}}
		srv.fact.Store(-2)
		h.bridge.AttachRootClusters([]contract.ClusterServer{srv})
		conn, replies := h.dial()
		if err := conn.Send(h.sealedInvoke(0x341)); err != nil {
			t.Fatal(err)
		}
		waitReply(t, replies)
		if got := srv.fact.Load(); got != 1 {
			t.Errorf("fact %d, want 1 (stated: large messages)", got)
		}
	})
}

// TestRouteCarriesLargeMessages: a TCP connection does, a UDP address, a
// reliable connection that is not TCP (BTP) and no route do not.
func TestRouteCarriesLargeMessages(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	if routeCarriesLargeMessages(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}) {
		t.Error("a UDP route carries large messages")
	}
	if !routeCarriesLargeMessages(b.routeFor(&fakeConn{})) {
		t.Error("a TCP connection does not carry large messages")
	}
	if routeCarriesLargeMessages(b.routeFor(&bleLikeConn{})) {
		t.Error("a BTP connection carries large messages")
	}
	if routeCarriesLargeMessages(nil) {
		t.Error("no route carries large messages")
	}
}
