// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// The reply route of an exchange (routing.exchangeSrcs) exists for the
// ack pump: it is where the standalone ack of an owed obligation goes.
// dischargeOwedAck and the pump release it with the obligation. The
// paths that piggyback the owed ack through LookupAndDischarge did not
// (review II, W1-conc-08): a route they left behind stayed until its
// session was evicted — one per Subscribe exchange, whose
// SubscribeResponse is the last reliable message and is answered by a
// standalone ack that stores nothing.

// routesOfExchange counts the reply routes held for (session, exchange),
// either role.
func routesOfExchange(b *Bridge, sessionID, exchangeID uint16) int {
	n := 0
	b.routing.exchangeSrcs.Range(func(k, _ any) bool {
		if key, ok := k.(mrp.ExchangeKey); ok && key.SessionID == sessionID && key.ExchangeID == exchangeID {
			n++
		}
		return true
	})
	return n
}

// TestPiggybackedAckReleasesTheReplyRoute: the two helpers that take an
// owed ack for a piggyback release the route with it.
func TestPiggybackedAckReleasesTheReplyRoute(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.mu.Lock()
	b.ackTracker = mrp.NewAckTracker(time.Hour) // obligations only; no pump
	b.mu.Unlock()
	peerConn, peerAddr := openPeerSocket(t)
	defer peerConn.Close()

	t.Run("refreshAckCounter", func(t *testing.T) {
		const exchangeID uint16 = 21
		b.owedInboundAck(peerAddr, buildMsgHdr(700), buildNeedsAckProto(exchangeID, 700))
		if n := routesOfExchange(b, 0, exchangeID); n != 1 {
			t.Fatalf("routes after the inbound message = %d, want 1", n)
		}
		hdr := message.Header{SessionID: 0, MessageCounter: 1}
		b.refreshAckCounter(&hdr, exchangeID, true)
		if hdr.MessageCounter != 700 {
			t.Fatalf("refreshed counter = %d, want 700", hdr.MessageCounter)
		}
		if n := routesOfExchange(b, 0, exchangeID); n != 0 {
			t.Errorf("routes after the ack was taken for a piggyback = %d, want 0 — nothing is owed on the exchange", n)
		}
	})
	t.Run("piggybackOwedAck", func(t *testing.T) {
		const exchangeID uint16 = 22
		b.owedInboundAck(peerAddr, buildMsgHdr(701), buildNeedsAckProto(exchangeID, 701))
		got := b.piggybackOwedAck(subTarget{sessionID: 0, exchangeID: exchangeID}, true)
		if !got.hasAck || got.ackCounter != 701 {
			t.Fatalf("piggyback = (%v, %d), want (true, 701)", got.hasAck, got.ackCounter)
		}
		if n := routesOfExchange(b, 0, exchangeID); n != 0 {
			t.Errorf("routes after the ack was taken for a piggyback = %d, want 0 — nothing is owed on the exchange", n)
		}
	})
}

// TestSubscribeExchangeLeavesNoReplyRoute drives a whole Subscribe over
// the receive path: SubscribeRequest, the priming ReportData, the peer's
// StatusResponse, the SubscribeResponse that piggybacks its ack. Nothing
// is owed on the exchange afterwards, so no route of it may remain.
func TestSubscribeExchangeLeavesNoReplyRoute(t *testing.T) {
	t.Parallel()
	const (
		sessionID  uint16 = 91
		exchangeID uint16 = 0x2191
	)
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour)) // attached after Start: no pump
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	markPASE(b, sessionID)

	// One reportable attribute of the first bridged endpoint: a report
	// of one chunk.
	var paths []im.ConcreteAttributePath
	for _, ep := range b.Topology().Endpoints {
		if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
			paths = endpointpkg.ReportablePaths(ep)[:1]
			break
		}
	}
	if len(paths) != 1 {
		t.Fatal("the topology has no bridged endpoint with a reportable path")
	}
	enc := tlv.NewEncoder()
	im.SubscribeRequest{MaxIntervalCeiling: 60, AttributeRequests: paths}.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatalf("SubscribeRequest.MarshalTLV: %v", err)
	}
	request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchangeID, Initiator: true, NeedsAck: true,
	}, payload)
	done := make(chan error, 1)
	go func() { done <- b.dispatch(context.Background(), request, peerAddr) }()

	report, _, ok := readOneIMDatagram(t, peerConn, peerSess, 5*time.Second)
	if !ok || report.Opcode != im.OpcodeReportData {
		t.Fatalf("first reply = (%v, opcode %#x), want the priming ReportData", ok, report.Opcode)
	}
	status, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		t.Fatal(err)
	}
	answer := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeStatusResponse, ExchangeID: exchangeID, Initiator: true, NeedsAck: true,
	}, status)
	if err := b.dispatch(context.Background(), answer, peerAddr); err != nil {
		t.Fatalf("dispatch of the StatusResponse: %v", err)
	}
	response, _, ok := readOneIMDatagram(t, peerConn, peerSess, 5*time.Second)
	if !ok || response.Opcode != im.OpcodeSubscribeResponse {
		t.Fatalf("second reply = (%v, opcode %#x), want the SubscribeResponse", ok, response.Opcode)
	}
	if !response.HasAck {
		t.Fatal("the SubscribeResponse piggybacks no ack; the test needs the piggyback path")
	}
	if err := <-done; err != nil {
		t.Fatalf("dispatch of the SubscribeRequest: %v", err)
	}
	if b.ackTracker.Owes(sessionID, exchangeID, false) {
		t.Fatal("an ack is still owed on the Subscribe exchange; the test needs it piggybacked")
	}
	if n := routesOfExchange(b, sessionID, exchangeID); n != 0 {
		t.Errorf("reply routes of the finished Subscribe exchange = %d, want 0", n)
	}
}
