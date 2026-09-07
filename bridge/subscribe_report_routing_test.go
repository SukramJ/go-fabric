// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for the routing half of the ongoing-report paths:
// which exchange an ongoing report rides and which peer address it is
// sent to. Lives in package bridge to reach the reporters and the
// routing tables directly.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// readOneIMDatagram waits for one datagram on peerConn, decrypts it
// with peerSess and returns its protocol header plus the IM payload.
func readOneIMDatagram(t *testing.T, peerConn *net.UDPConn, peerSess *channel.Session, wait time.Duration) (message.ProtocolHeader, []byte, bool) {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	if err := peerConn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, _, err := peerConn.ReadFromUDP(buf)
	if err != nil {
		return message.ProtocolHeader{}, nil, false
	}
	hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
	if err != nil {
		t.Fatalf("peer.Decrypt: %v", err)
	}
	proto, protoLen, err := message.UnmarshalProtocolHeader(plain)
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	return proto, plain[protoLen:], true
}

// encryptIMFromPeer seals an IM message from the peer for the bridge's
// local session id and returns the wire datagram.
func encryptIMFromPeer(t *testing.T, peerSess *channel.Session, localSessionID uint16, proto message.ProtocolHeader, payload []byte) []byte {
	t.Helper()
	proto.ProtocolID = im.InteractionModelProtocolID
	body := append(proto.Marshal(), payload...)
	hdr := message.Header{SessionID: localSessionID}
	enc, err := peerSess.Encrypt(&hdr, securityFlagsByte(&hdr), body)
	if err != nil {
		t.Fatalf("peer Encrypt: %v", err)
	}
	return append(hdr.Marshal(), enc.Ciphertext...)
}

// TestReportSubscriptionEventsRidesAFreshBridgeInitiatedExchange pins
// that an ongoing EVENT report opens a fresh bridge-initiated exchange,
// exactly like the attribute path. The commissioner closed its
// Subscribe exchange when the SubscribeResponse landed; a report sent
// there with Initiator=false is an unsolicited message on an unknown
// exchange, which matter.js answers with a standalone ack and drops
// (packages/protocol/src/protocol/ExchangeManager.ts:411-418). Ongoing
// server reports are always new server-initiated exchanges
// (packages/node/src/node/server/ServerSubscription.ts:823
// initiateExchange).
func TestReportSubscriptionEventsRidesAFreshBridgeInitiatedExchange(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 75
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	const (
		subID             uint32 = 8585
		subscribeExchange uint16 = 17
	)
	b.routing.subTargets.Store(subID, subTarget{
		src:                 peerAddr,
		hasPeerSourceNodeID: true,
		peerSourceNodeID:    0xBBBB4444,
		exchangeID:          subscribeExchange,
		sessionID:           sessionID,
		peerInitiator:       true,
	})

	b.reportSubscriptionEvents(context.Background(), &subscription.Subscription{ID: subID}, oneEventReport())

	proto, _, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok {
		t.Fatal("no event report reached the peer")
	}
	if proto.Opcode != im.OpcodeReportData {
		t.Fatalf("opcode = %#x, want ReportData %#x", proto.Opcode, im.OpcodeReportData)
	}
	if !proto.Initiator {
		t.Error("event report sent with Initiator=false; an ongoing report must open a bridge-initiated exchange")
	}
	if proto.ExchangeID == subscribeExchange {
		t.Errorf("event report sent on the commissioner's Subscribe exchange %d; want a fresh exchange id", subscribeExchange)
	}
}

// TestOngoingReportsFollowThePeerAddressOfLaterInboundTraffic pins that
// the address an ongoing report goes to is the one the peer most
// recently used on the session, not the one frozen at Subscribe time.
// A controller whose source address changes inside one CASE session
// (IPv6 temporary-address rotation, interface switch) keeps sending
// authenticated traffic from the new address; matter.js adopts it —
// "the new message wins" (packages/protocol/src/protocol/
// ExchangeManager.ts:400-405) — and routes subscription reports through
// the session's current channel (ServerSubscription.ts:823).
func TestOngoingReportsFollowThePeerAddressOfLaterInboundTraffic(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 76
	b := newStartedBridge(t)
	oldConn, peerSess, oldAddr := chunkTestPeer(t, b, sessionID)

	newConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = newConn.Close() })
	newAddr, ok := newConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected addr type %T", newConn.LocalAddr())
	}

	const subID uint32 = 8686
	b.routing.subTargets.Store(subID, subTarget{
		src:                 oldAddr,
		hasPeerSourceNodeID: true,
		peerSourceNodeID:    0xBBBB4444,
		exchangeID:          18,
		sessionID:           sessionID,
		peerInitiator:       true,
	})

	// The peer moves: an authenticated IM datagram on the same session
	// arrives from the new address.
	status, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		t.Fatalf("EncodeStatusResponse: %v", err)
	}
	datagram := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeStatusResponse, ExchangeID: 0x1234,
	}, status)
	if err := b.dispatch(context.Background(), datagram, newAddr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	b.reportSubscriptionEvents(context.Background(), &subscription.Subscription{ID: subID}, oneEventReport())

	if _, _, got := readOneIMDatagram(t, newConn, peerSess, time.Second); !got {
		t.Error("no report reached the peer's current address")
	}
	if _, _, got := readOneIMDatagram(t, oldConn, peerSess, 200*time.Millisecond); got {
		t.Error("a report was still sent to the address frozen at Subscribe time")
	}
}
