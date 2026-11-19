// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestRequestWithoutAckOfAnOutstandingReplyIsNotProcessed (review II,
// phase 7, V3): a message on an exchange where this node's previous
// reliable message still waits for its acknowledgement, and which carries
// no acknowledgement itself, is refused on receive — never processed — as
// matter.js refuses it (packages/protocol/src/protocol/MessageExchange.ts:521-525
// at db021da0, "Previous message ack is missing", thrown before the message
// reaches the exchange's queue). Before, the Write took effect and only its
// WriteResponse was refused at the send (TrackOnExchange).
func TestRequestWithoutAckOfAnOutstandingReplyIsNotProcessed(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour)) // attached after Start: no pump
	p := newIMPeer(t, b)
	const x = 0x4301
	p.send(x, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
	if got := p.status(); got != im.StatusSuccess {
		t.Fatalf("TimedRequest: %v", got)
	}
	if !b.outboundReliable.holdsSecureExchange(paseTestSessionID, x, exchangeResponder) {
		t.Fatal("the TimedRequest's StatusResponse is not awaiting its acknowledgement; the test needs it to")
	}
	// The timed Write, without a piggybacked acknowledgement of that
	// StatusResponse.
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeWriteRequest, ExchangeID: x, Initiator: true}.Marshal()
	payload := writeRequestBytes(t, im.WriteRequest{TimedRequest: true, Writes: []im.AttributeWrite{
		{Path: onTimePath(), Value: im.AttributeValue{Value: uint16(7)}},
	}})
	err := b.dispatch(context.Background(), secureTestDatagram(t, b, proto, payload), p.conn.LocalAddr())
	if !p.silent(300 * time.Millisecond) {
		t.Error("the bridge answered a request that does not acknowledge its outstanding reply")
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes, want 0: a request that does not acknowledge the outstanding reply was processed (dispatch: %v)", n, err)
	}
	if !errors.Is(err, errPreviousAckMissing) {
		t.Errorf("dispatch: %v, want errPreviousAckMissing", err)
	}
	// The StatusResponse still waits for its acknowledgement: the refused
	// message did not displace it.
	if !b.outboundReliable.holdsSecureExchange(paseTestSessionID, x, exchangeResponder) {
		t.Error("the refused message cleared the outstanding StatusResponse")
	}

	// The same Write acknowledging the StatusResponse is processed and
	// answered.
	ackFor, ok := pendingCounterOfExchange(b, paseTestSessionID, x)
	if !ok {
		t.Fatal("the StatusResponse no longer waits for its acknowledgement")
	}
	proto = message.ProtocolHeader{
		ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeWriteRequest, ExchangeID: x, Initiator: true,
		HasAck: true, AckCounter: ackFor,
	}.Marshal()
	if err := b.dispatch(context.Background(), secureTestDatagram(t, b, proto, payload), p.conn.LocalAddr()); err != nil {
		t.Fatalf("dispatch of the acknowledging Write: %v", err)
	}
	if reply, _, ok := p.next(2 * time.Second); !ok || reply.Opcode != im.OpcodeWriteResponse {
		t.Fatalf("the acknowledging Write was not answered with a WriteResponse (ok %t, opcode 0x%02X)", ok, reply.Opcode)
	}
	if n := srv.writes(); n != 1 {
		t.Fatalf("%d writes after the acknowledging Write, want 1", n)
	}
}
