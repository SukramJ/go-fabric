// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestFlushedReportSentOnceUntracked: a report flushed as its fabric
// leaves asks the peer for an ack but is not handed to the outbound
// reliable tracker — no retransmission, no counter→subscription entry —
// and the peer's ack for it is a no-op. Mirrors matter.js's
// `disableMrpLogic` (packages/protocol/src/protocol/MessageExchange.ts
// :785-798 at db021da0), set by sendDataReportMessage for a closed
// subscription's report (InteractionMessenger.ts:840) (issue 473).
func TestFlushedReportSentOnceUntracked(t *testing.T) {
	t.Parallel()
	bridgeKey := bytes.Repeat([]byte{0xAA}, 16)
	peerKey := bytes.Repeat([]byte{0xBB}, 16)
	bridgeSess, err := channel.New(channel.Config{EncryptKey: bridgeKey, DecryptKey: peerKey, LocalNodeID: 1, PeerNodeID: 2, InitialCounter: 100})
	if err != nil {
		t.Fatalf("bridge session: %v", err)
	}
	peerSess, err := channel.New(channel.Config{EncryptKey: peerKey, DecryptKey: bridgeKey, LocalNodeID: 2, PeerNodeID: 1, InitialCounter: 200})
	if err != nil {
		t.Fatalf("peer session: %v", err)
	}
	listener, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatalf("udp.New: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("peer ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })

	const sessionID uint16 = 1
	b := &Bridge{}
	b.listener = listener
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
		return bridgeSess, id == sessionID
	})
	b.outboundReliable = newOutboundReliableTracker(nil, nil)

	target := subTarget{src: peerConn.LocalAddr(), exchangeID: 7, sessionID: sessionID, peerInitiator: true, flush: true}
	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  0x99,
		Reports: []im.AttributeReport{{
			Path:        im.ConcreteAttributePath{Endpoint: 3, Cluster: 0x0006, Attribute: 0x0000},
			DataVersion: 12,
			Value:       im.AttributeValue{Value: true},
		}},
	}
	counters, exchangeID, err := b.sendInitiatedReport(target, report)
	if err != nil {
		t.Fatalf("sendInitiatedReport: %v", err)
	}
	if len(counters) != 0 {
		t.Fatalf("the flushed report returned counters %v, want none (untracked)", counters)
	}
	if n := b.outboundReliable.Pending(); n != 0 {
		t.Fatalf("the tracker holds %d entries after a flushed report, want 0", n)
	}
	owned := 0
	b.reportCounterOwner.Range(func(any, any) bool { owned++; return true })
	if owned != 0 {
		t.Fatalf("%d counter→subscription entries after a flushed report, want 0", owned)
	}

	if err := peerConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("peer ReadFromUDP: %v", err)
	}
	hdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), buf[hdrLen:n])
	if err != nil {
		t.Fatalf("peer.Decrypt: %v", err)
	}
	proto, _, err := message.UnmarshalProtocolHeader(plain)
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if !proto.NeedsAck || proto.ExchangeID != exchangeID || proto.Opcode != im.OpcodeReportData {
		t.Fatalf("flushed report: NeedsAck=%v exchange %d opcode %#x, want an ack-requesting ReportData on exchange %d",
			proto.NeedsAck, proto.ExchangeID, proto.Opcode, exchangeID)
	}
	if b.outboundReliable.Ack(sessionID, hdr.MessageCounter) {
		t.Fatal("the ack of the flushed report found a tracker entry")
	}
}
