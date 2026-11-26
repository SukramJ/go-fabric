// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors in the acknowledgement and route
// bookkeeping: ackpump.go, receive.go's inboundAckRoute, securechannel.go's
// sendHandshakeFailureReport, pase_deferred_close.go's peerExchangeHeld and
// exchange_routing.go's sweep throttle (phase 8 wave 2b, #602, decision
// 692).

package bridge

import (
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// trackedBridgeW2B2b is a started bridge with an obligation-only ack
// tracker.
func trackedBridgeW2B2b(t *testing.T) (*Bridge, *mrp.AckTracker) {
	t.Helper()
	b := newStartedBridge(t)
	tracker := mrp.NewAckTracker(time.Hour) // obligations only; no pump
	b.mu.Lock()
	b.ackTracker = tracker
	b.mu.Unlock()
	return b, tracker
}

// TestHandshakeReplyLeavesAnotherInitiatorsAckW2B2b: a reply on the
// unsecured session discharges the exchange's obligation only when the
// exchange's route is the replying handshake's initiator's. Kills
// ackpump.go:390 (guard off).
func TestHandshakeReplyLeavesAnotherInitiatorsAckW2B2b(t *testing.T) {
	t.Parallel()
	b, tracker := trackedBridgeW2B2b(t)
	const exchange = 0x5100
	route := mrp.ExchangeKey{SessionID: 0, ExchangeID: exchange, Initiator: false}
	b.routing.exchangeSrcs.Store(route, exchangeReplyTarget{
		src: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, hasPeerSourceNodeID: true, peerSourceNodeID: 0xA,
	})
	tracker.Owe(1, 0, exchange, false, time.Now())

	b.dischargeOwedReplyAck(&message.Header{HasSourceNodeID: true, SourceNodeID: 0xB}, message.ProtocolHeader{ExchangeID: exchange, Initiator: true})
	if !tracker.Owes(0, exchange, false) {
		t.Fatal("a reply to initiator 0xB discharged the acknowledgement owed to initiator 0xA")
	}
}

// TestDuplicateAckRouteIsTheExchangesW2B2b: a secure session's duplicate
// is acknowledged at the route its exchange already has, ahead of the
// session's route. Kills receive.go:620 (drop !) and :621 (guard off).
func TestDuplicateAckRouteIsTheExchangesW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	exchangeRoute := &net.UDPAddr{IP: net.IPv6loopback, Port: 1111}
	sessionRoute := &net.UDPAddr{IP: net.IPv6loopback, Port: 2222}
	b.routing.exchangeSrcs.Store(mrp.ExchangeKey{SessionID: 5, ExchangeID: 9, Initiator: false}, exchangeReplyTarget{src: exchangeRoute})
	b.sessionPeerAddrs.Store(uint16(5), net.Addr(sessionRoute))

	got := b.inboundAckRoute(&net.UDPAddr{IP: net.IPv6loopback, Port: 3333}, &message.Header{SessionID: 5}, message.ProtocolHeader{ExchangeID: 9, Initiator: true}, true)
	if got != net.Addr(exchangeRoute) {
		t.Fatalf("duplicate acknowledged at %v, want the exchange's route %v", got, exchangeRoute)
	}
}

// TestHandshakeFailureReportAckAndHoldW2B2b: a failure report that went
// out discharges the request's acknowledgement, one with no route returns
// the error, and while a handshake reply waits for its acknowledgement
// no report is sent at all. Kills securechannel.go:1953 (guard off) and
// :1957 (!= → ==).
func TestHandshakeFailureReportAckAndHoldW2B2b(t *testing.T) {
	t.Parallel()
	b, tracker := trackedBridgeW2B2b(t)
	body := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), 0, nil)
	hdr := &message.Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 0xA}

	proto := message.ProtocolHeader{ProtocolID: mrp.SecureChannelProtocolID, Opcode: mrp.SCOpcodeSigma1, ExchangeID: 0x5200, Initiator: true}
	tracker.Owe(1, 0, proto.ExchangeID, false, time.Now())
	if err := b.sendHandshakeFailureReport(&connRoute{conn: &fakeConn{}}, hdr, proto, body, nil); err != nil {
		t.Fatalf("sendHandshakeFailureReport: %v", err)
	}
	if tracker.Owes(0, proto.ExchangeID, false) {
		t.Error("the acknowledgement of the request is still owed after the report went out")
	}

	proto.ExchangeID = 0x5201
	if err := b.sendHandshakeFailureReport(&connRoute{}, hdr, proto, body, nil); err == nil {
		t.Error("a report with no route to the peer returned no error")
	}

	// A reliable handshake reply over UDP waits for its acknowledgement
	// on exchange 0x5202; the failure report on that exchange is not
	// sent, whatever route it would take.
	_, peerAddr := newSubscribeTestPeer(t)
	b.mu.Lock()
	b.outboundReliable = newOutboundReliableTracker(nil, nil)
	b.mu.Unlock()
	proto.ExchangeID = 0x5202
	if err := b.sendHandshakeReplyReliable(peerAddr, hdr, proto, mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil, 0); err != nil {
		t.Fatalf("sendHandshakeReplyReliable: %v", err)
	}
	c := &fakeConn{}
	if err := b.sendHandshakeFailureReport(&connRoute{conn: c}, hdr, proto, body, nil); err != nil {
		t.Fatalf("sendHandshakeFailureReport: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 0 {
		t.Fatalf("%d failure reports sent while the handshake's reply waits for its acknowledgement, want none", len(c.sent))
	}
}

// TestPeerExchangeHeldByAStatusResponseWaitW2B2b: an exchange whose
// StatusResponse is awaited holds state. Kills pase_deferred_close.go:387
// (guard off).
func TestPeerExchangeHeldByAStatusResponseWaitW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.routing.statusResponseWaits.Store(mrp.ExchangeKey{SessionID: 7, ExchangeID: 8, Initiator: false}, make(chan statusAnswer, 1))
	if !b.peerExchangeHeld(7, 8) {
		t.Fatal("an exchange with an armed StatusResponse wait holds no state")
	}
}

// TestTimedSweepRunsOncePerIntervalW2B2b: the sweep runs on the first
// call, not again before timedSweepInterval has passed, and again once it
// has, to the nanosecond. Kills exchange_routing.go:191 (guard off,
// < → <=).
func TestTimedSweepRunsOncePerIntervalW2B2b(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Now()
	expired := func(ex uint16) { r.timedDeadlines.Store(timedKey{sessionID: 1, exchangeID: ex}, now.Add(-time.Second)) }
	const start = time.Hour

	expired(1)
	if n := r.maybeSweepExpiredTimedDeadlines(now, start); n != 1 {
		t.Fatalf("first sweep reclaimed %d, want 1", n)
	}
	expired(2)
	if n := r.maybeSweepExpiredTimedDeadlines(now, start+timedSweepInterval-1); n != 0 {
		t.Fatalf("a sweep inside the interval reclaimed %d, want 0", n)
	}
	if n := r.maybeSweepExpiredTimedDeadlines(now, start+timedSweepInterval); n != 1 {
		t.Fatalf("the sweep a full interval later reclaimed %d, want 1", n)
	}
}
