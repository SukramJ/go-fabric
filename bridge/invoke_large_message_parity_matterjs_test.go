// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A command with the Large Message quality runs only over a transport that
// carries large messages: matter.js answers it INVALID_TRANSPORT_TYPE on any
// other session (packages/protocol/src/action/server/CommandInvokeResponse.ts:281-285
// at db021da0), the session's flag being its channel's — TCP true
// (packages/protocol/src/transport/tcp/TcpChannel.ts:45), UDP false
// (packages/general/src/net/udp/UdpTransport.ts:69), BLE false
// (packages/protocol/src/ble/Ble.ts:39).

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/btp"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestInvokeLargeMessageOverUDPParity drives a real Bridge.dispatch with
// TlsCertificateManagement.ProvisionRootCertificate (quality "L") over the
// UDP test session: the command's path answers INVALID_TRANSPORT_TYPE and
// the server never runs it.
func TestInvokeLargeMessageOverUDPParity(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	srv := &countingInvokeServer{id: 0x0801}
	b.AttachRootClusters([]contract.ClusterServer{srv})

	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeInvokeRequest)
	payload := encodeTimedTestInvokeRequest(t, false, im.ConcreteCommandPath{
		Endpoint: 0, Cluster: 0x0801, Command: 0x00, // ProvisionRootCertificate
		HasEndpoint: true, HasCluster: true, HasCommand: true,
	})
	if err := b.dispatch(context.Background(), secureTestDatagram(t, b, proto, payload), peerConn.LocalAddr()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	rbuf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(rbuf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	got := openTestReplyFor(t, b, rbuf[:n])
	_, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	rproto, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if rproto.Opcode != im.OpcodeInvokeResponse {
		t.Fatalf("reply opcode = 0x%02X, want InvokeResponse", rproto.Opcode)
	}
	if status, found := firstStatusIBCode(t, got[hdrLen+protoLen:]); !found || status != im.StatusInvalidTransportType {
		t.Errorf("path status = %v (found %v), want INVALID_TRANSPORT_TYPE (0xD1)", status, found)
	}
	if n := srv.invokes.Load(); n != 0 {
		t.Errorf("server ran %d commands, want 0", n)
	}
}

// TestRouteCarriesLargeMessages: a TCP connection carries large messages, a
// UDP datagram and a BTP connection do not.
func TestRouteCarriesLargeMessages(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		src  net.Addr
		want bool
	}{
		{"UDP datagram", &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, false},
		{"UDP connection route", &connRoute{conn: &udp.Conn{}}, false},
		{"BTP connection", &connRoute{conn: &btp.Conn{}}, false},
		{"TCP connection", &connRoute{conn: &tcp.Conn{}}, true},
		{"nil route", (*connRoute)(nil), false},
	} {
		if got := routeCarriesLargeMessages(c.src); got != c.want {
			t.Errorf("%s: %t, want %t", c.name, got, c.want)
		}
	}
}

// TestInvokeLargeMessageOverTCPParity: the same command over a session
// bound to a TCP connection runs and is answered on the connection.
func TestInvokeLargeMessageOverTCPParity(t *testing.T) {
	h := newTCPHarness(t)
	srv := &countingInvokeServer{id: 0x0801}
	h.bridge.AttachRootClusters([]contract.ClusterServer{srv})
	conn, replies := h.dial()
	body := encodeTimedTestInvokeRequest(t, false, im.ConcreteCommandPath{
		Endpoint: 0, Cluster: 0x0801, Command: 0x00, // ProvisionRootCertificate
		HasEndpoint: true, HasCluster: true, HasCommand: true,
	})
	proto := message.ProtocolHeader{
		Initiator: true, Opcode: im.OpcodeInvokeRequest,
		ExchangeID: 0x330, ProtocolID: im.InteractionModelProtocolID,
	}
	hdr := message.Header{SessionID: harnessLocalSessionID}
	sealed, err := h.peer.Encrypt(&hdr, securityFlagsByte(&hdr), append(proto.Marshal(), body...))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Send(append(hdr.Marshal(), sealed.Ciphertext...)); err != nil {
		t.Fatal(err)
	}
	if got := h.open(waitReply(t, replies)); got.Opcode != im.OpcodeInvokeResponse {
		t.Fatalf("reply opcode = 0x%02X, want InvokeResponse", got.Opcode)
	}
	if n := srv.invokes.Load(); n != 1 {
		t.Errorf("server ran %d commands, want 1", n)
	}
}
