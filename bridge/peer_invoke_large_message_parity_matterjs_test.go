// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A command with the Large Message quality that this node sends to a peer
// goes over a TCP session only: matter.js's client requires the TCP
// transport for it (packages/protocol/src/action/client/ClientInteraction.ts:716-719,
// :1235 at db021da0) and takes only a session of that transport, never
// falling back (packages/protocol/src/peer/Peer.ts:374-375, :440-448).

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// webRTCRequestorAnswer is WebRtcTransportRequestor (0x0554) Answer (0x01),
// a command with the Large Message quality (schema/large_message_gen.go).
const (
	webRTCRequestorCluster = uint32(0x0554)
	webRTCRequestorAnswer  = uint32(0x01)
)

// sentInvokePath decrypts one message the bridge sent under the harness
// session and returns the path of its InvokeRequest's one command.
func (h *tcpHarness) sentInvokePath(msg []byte) im.ConcreteCommandPath {
	h.t.Helper()
	hdr, n, err := message.UnmarshalHeader(msg)
	if err != nil {
		h.t.Fatal(err)
	}
	body, _, err := h.peer.Decrypt(&hdr, securityFlagsByte(&hdr), append([]byte(nil), msg[n:]...))
	if err != nil {
		h.t.Fatal(err)
	}
	proto, protoLen, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		h.t.Fatal(err)
	}
	if proto.Opcode != im.OpcodeInvokeRequest {
		h.t.Fatalf("sent opcode 0x%02X, want an InvokeRequest", proto.Opcode)
	}
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(body[protoLen:]), nil)
	if err != nil || len(req.Invokes) != 1 {
		h.t.Fatalf("sent InvokeRequest %+v, %v", req, err)
	}
	return req.Invokes[0].Path
}

// TestPeerInvokeLargeMessageOverUDPRefused: the peer's only session runs
// over UDP. A Large Message command is refused with
// [ErrLargeMessageTransport] and not sent; the first message on the wire
// is the ordinary command that follows it.
func TestPeerInvokeLargeMessageOverUDPRefused(t *testing.T) {
	h := newTCPHarness(t)
	h.bridge.AttachSessionRegistry(&peerListingRegistry{fakeSessionRegistry: h.registry, ids: []uint16{harnessLocalSessionID}})
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	h.bridge.sessionPeerAddrs.Store(harnessLocalSessionID, peerConn.LocalAddr())
	fabricIndex, _ := h.bridge.sessions.(*harnessLookup).FabricFor(harnessLocalSessionID)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s, err := h.bridge.ConnectPeer(ctx, fabricIndex, harnessControllerNodeID)
	if err != nil {
		t.Fatalf("ConnectPeer: %v", err)
	}
	if _, err := s.Invoke(ctx, 1, webRTCRequestorCluster, webRTCRequestorAnswer, nil); !errors.Is(err, ErrLargeMessageTransport) {
		t.Fatalf("Large Message command over a UDP session: %v, want ErrLargeMessageTransport", err)
	}

	// Control: OnOff.On has no Large Message quality and goes out over UDP.
	onCtx, onCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := s.Invoke(onCtx, 1, 0x0006, 0x01, nil)
		done <- err
	}()
	_ = peerConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	if p := h.sentInvokePath(buf[:n]); p.Cluster != 0x0006 || p.Command != 0x01 {
		t.Fatalf("first command on the wire %+v, want OnOff.On (the Large Message command must not be sent)", p)
	}
	onCancel()
	<-done
}

// TestPeerInvokeLargeMessageOverTCP: the peer's session is bound to a TCP
// connection; the Large Message command goes out on it.
func TestPeerInvokeLargeMessageOverTCP(t *testing.T) {
	h := newTCPHarness(t)
	h.bridge.AttachSessionRegistry(&peerListingRegistry{fakeSessionRegistry: h.registry, ids: []uint16{harnessLocalSessionID}})
	h.bridge.mu.RLock()
	tl := h.bridge.tcpListener
	h.bridge.mu.RUnlock()
	fabricIndex, _ := h.bridge.sessions.(*harnessLookup).FabricFor(harnessLocalSessionID)

	received := make(chan []byte, 4)
	var d tcp.Dialer
	conn, err := d.Dial(t.Context(), tl.Addr().String(), func(_ *tcp.Conn, msg []byte, _ func()) { received <- msg })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Bind the session to the connection: its first message arrives on it.
	if err := conn.Send(h.sealedInvoke(0x320)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, received)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s, err := h.bridge.ConnectPeer(ctx, fabricIndex, harnessControllerNodeID)
	if err != nil {
		t.Fatalf("ConnectPeer: %v", err)
	}
	invokeCtx, invokeCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := s.Invoke(invokeCtx, 1, webRTCRequestorCluster, webRTCRequestorAnswer, nil)
		done <- err
	}()
	var sent []byte
	select {
	case sent = <-received:
	case err := <-done:
		t.Fatalf("Large Message command over a TCP session: %v, want it sent", err)
	case <-ctx.Done():
		t.Fatal("no Large Message command on the TCP connection")
	}
	if p := h.sentInvokePath(sent); p.Cluster != webRTCRequestorCluster || p.Command != webRTCRequestorAnswer {
		t.Fatalf("sent %+v, want WebRtcTransportRequestor.Answer", p)
	}
	invokeCancel()
	<-done
}
