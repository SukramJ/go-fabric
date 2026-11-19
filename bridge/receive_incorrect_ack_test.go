// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestRequestWithAnIncorrectAckIsNotProcessed (review II, phase 7,
// decision 276): a message on an exchange whose reliable message from this
// node still waits for its acknowledgement, and whose acknowledgement names
// another counter, is refused on receive — not acknowledged, not
// processed, the outstanding message kept — and the peer's retransmission
// of it is acknowledged as a duplicate and not processed either; a
// standalone acknowledgement that names another counter is ignored and
// clears nothing. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:526-533 at db021da0
// ("Incorrect ack received", the standalone ack excepted at :528-529), the
// duplicate branch at :501-511; matter.js has no test of it (Fable's note,
// Q4), so the case is this module's own.
func TestRequestWithAnIncorrectAckIsNotProcessed(t *testing.T) {
	t.Parallel()
	b, srv := onTimeBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour)) // attached after Start: no pump
	p := newIMPeer(t, b)
	const x = 0x4302
	p.send(x, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
	if got := p.status(); got != im.StatusSuccess {
		t.Fatalf("TimedRequest: %v", got)
	}
	outstanding, ok := pendingCounterOfExchange(b, paseTestSessionID, x)
	if !ok {
		t.Fatal("the TimedRequest's StatusResponse is not awaiting its acknowledgement; the test needs it to")
	}
	payload := writeRequestBytes(t, im.WriteRequest{TimedRequest: true, Writes: []im.AttributeWrite{
		{Path: onTimePath(), Value: im.AttributeValue{Value: uint16(7)}},
	}})
	wrong := message.ProtocolHeader{
		ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeWriteRequest, ExchangeID: x, Initiator: true,
		NeedsAck: true, HasAck: true, AckCounter: outstanding + 1,
	}.Marshal()
	datagram := secureTestDatagram(t, b, wrong, payload)
	err := b.dispatch(context.Background(), datagram, p.conn.LocalAddr())
	if !errors.Is(err, errIncorrectAck) {
		t.Fatalf("dispatch: %v, want errIncorrectAck", err)
	}
	if !p.silent(300 * time.Millisecond) {
		t.Error("the bridge answered a request whose acknowledgement names another counter")
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes, want 0: a request with an incorrect acknowledgement was processed", n)
	}
	if b.ackTracker.Owes(paseTestSessionID, x, false) {
		t.Error("the refused request is owed an acknowledgement")
	}
	if c, ok := pendingCounterOfExchange(b, paseTestSessionID, x); !ok || c != outstanding {
		t.Fatalf("the outstanding StatusResponse is %d (held %t), want %d kept", c, ok, outstanding)
	}

	// Its retransmission — the same datagram — is a duplicate: acknowledged
	// at once, not processed.
	if err := b.dispatch(context.Background(), datagram, p.conn.LocalAddr()); err != nil {
		t.Fatalf("dispatch of the retransmission: %v", err)
	}
	if n := srv.writes(); n != 0 {
		t.Fatalf("%d writes after the retransmission, want 0", n)
	}

	// A standalone acknowledgement of another counter is ignored.
	ack := message.ProtocolHeader{
		ProtocolID: mrp.SecureChannelProtocolID, Opcode: mrp.StandaloneAckOpcode, ExchangeID: x, Initiator: true,
		HasAck: true, AckCounter: outstanding + 1,
	}.Marshal()
	if err := b.dispatch(context.Background(), secureTestDatagram(t, b, ack, nil), p.conn.LocalAddr()); err != nil {
		t.Fatalf("dispatch of a standalone ack of another counter: %v, want it passed on", err)
	}
	if c, ok := pendingCounterOfExchange(b, paseTestSessionID, x); !ok || c != outstanding {
		t.Fatalf("after a standalone ack of another counter the outstanding StatusResponse is %d (held %t), want %d kept", c, ok, outstanding)
	}

	// The Write acknowledging the StatusResponse is processed.
	right := message.ProtocolHeader{
		ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeWriteRequest, ExchangeID: x, Initiator: true,
		NeedsAck: true, HasAck: true, AckCounter: outstanding,
	}.Marshal()
	if err := b.dispatch(context.Background(), secureTestDatagram(t, b, right, payload), p.conn.LocalAddr()); err != nil {
		t.Fatalf("dispatch of the acknowledging Write: %v", err)
	}
	if n := srv.writes(); n != 1 {
		t.Fatalf("%d writes after the acknowledging Write, want 1", n)
	}
}

// TestAckClearsOnlyItsExchangesMessage (review II, phase 7, decision 278):
// an acknowledgement received on one exchange clears this node's message
// of that exchange only. A standalone acknowledgement on an exchange with
// nothing outstanding that names the counter of another exchange's
// outstanding message leaves that message in retransmission. Mirrors
// matter.js packages/protocol/src/protocol/MessageExchange.ts:521-542 at
// db021da0: the acknowledgement is evaluated inside its exchange.
func TestAckClearsOnlyItsExchangesMessage(t *testing.T) {
	t.Parallel()
	b, _ := onTimeBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour))
	p := newIMPeer(t, b)
	const x, y, z = 0x4303, 0x4304, 0x4305
	for _, ex := range []uint16{x, y} {
		p.send(ex, im.OpcodeTimedRequest, timedRequestBytes(t, 30000))
		if got := p.status(); got != im.StatusSuccess {
			t.Fatalf("TimedRequest on %#x: %v", ex, got)
		}
	}
	cy, ok := pendingCounterOfExchange(b, paseTestSessionID, y)
	if !ok {
		t.Fatal("the StatusResponse on y is not awaiting its acknowledgement")
	}
	cx, ok := pendingCounterOfExchange(b, paseTestSessionID, x)
	if !ok {
		t.Fatal("the StatusResponse on x is not awaiting its acknowledgement")
	}
	standalone := func(ex uint16, counter uint32) {
		t.Helper()
		proto := message.ProtocolHeader{
			ProtocolID: mrp.SecureChannelProtocolID, Opcode: mrp.StandaloneAckOpcode, ExchangeID: ex, Initiator: true,
			HasAck: true, AckCounter: counter,
		}.Marshal()
		if err := b.dispatch(context.Background(), secureTestDatagram(t, b, proto, nil), p.conn.LocalAddr()); err != nil {
			t.Fatalf("dispatch of a standalone ack on %#x: %v", ex, err)
		}
	}
	standalone(z, cy)
	if c, ok := pendingCounterOfExchange(b, paseTestSessionID, y); !ok || c != cy {
		t.Fatal("an acknowledgement on another exchange cleared y's StatusResponse")
	}
	standalone(x, cx)
	if _, ok := pendingCounterOfExchange(b, paseTestSessionID, x); ok {
		t.Fatal("x's own acknowledgement left its StatusResponse")
	}
	if _, ok := pendingCounterOfExchange(b, paseTestSessionID, y); !ok {
		t.Fatal("x's acknowledgement cleared y's StatusResponse")
	}
}

// TestAckOnExchangeIsExchangeScoped is decision 278 at the tracker: an
// acknowledgement clears the entry of its counter only for the slot it
// arrived on — the exchange and this node's role in it — or for the
// unnamed slot of that exchange id.
func TestAckOnExchangeIsExchangeScoped(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, clock.System{})
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	now := time.Now()
	if err := tr.TrackOnExchange(1, 7, 0x10, false, []byte{1}, dest, now); err != nil {
		t.Fatal(err)
	}
	if tr.AckOnExchange(7, 0x11, exchangeResponder, 1) {
		t.Fatal("an acknowledgement on exchange 0x11 cleared exchange 0x10's message")
	}
	if tr.AckOnExchange(7, 0x10, exchangeInitiator, 1) {
		t.Fatal("an acknowledgement on the other role's exchange of the id cleared the message")
	}
	if !tr.AckOnExchange(7, 0x10, exchangeResponder, 1) || tr.Pending() != 0 {
		t.Fatal("the acknowledgement on the message's own exchange did not clear it")
	}
	tr.Track(2, 7, 0x12, []byte{2}, dest, now)
	if !tr.AckOnExchange(7, 0x12, exchangeInitiator, 2) {
		t.Fatal("an acknowledgement on the exchange id of a message tracked without a role did not clear it")
	}
}

// TestHandshakeMessageWithAnIncorrectAckIsRefused (review II, phase 7,
// decision 277): the receive rules hold on the unsecured session too. A
// Pake1 whose acknowledgement names another counter than the
// PBKDFParamResponse waiting for one is refused — not acknowledged, not
// answered, the response kept; a standalone acknowledgement of another
// counter is ignored. matter.js runs PASE and CASE exchanges through the
// same MessageExchange.onMessageReceived (Fable's note, Q1;
// packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0).
func TestHandshakeMessageWithAnIncorrectAckIsRefused(t *testing.T) {
	t.Parallel()
	r, _ := newPaseClaimRig(t)
	holder := newPipelinePeer(t, r.b, 0xBAD)
	const xid = uint16(0x0103)
	key := HandshakeKey{InitiatorNodeID: 0xBAD, ExchangeID: xid}
	if err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	_, counter, _ := holder.recv(mrp.SCOpcodePBKDFParamResponse, "the PBKDFParamRequest")
	wrong := counter + 1
	err := holder.send(mrp.SCOpcodePake1, xid, []byte{0x15, 0x18}, &wrong)
	if !errors.Is(err, errIncorrectAck) {
		t.Fatalf("a Pake1 acknowledging another counter: dispatch %v, want errIncorrectAck", err)
	}
	holder.none(300*time.Millisecond, "a Pake1 acknowledging another counter")
	if r.b.ackTracker.Owes(0, xid, false) {
		t.Error("the refused Pake1 is owed an acknowledgement")
	}
	if got := r.b.outboundReliable.pendingOnHandshake(key); len(got) != 1 || got[0] != counter {
		t.Fatalf("the handshake's outstanding messages are %v, want the PBKDFParamResponse %d", got, counter)
	}
	if err := holder.send(mrp.StandaloneAckOpcode, xid, nil, &wrong); err != nil {
		t.Fatalf("a standalone ack of another counter: %v, want it passed on", err)
	}
	if got := r.b.outboundReliable.pendingOnHandshake(key); len(got) != 1 || got[0] != counter {
		t.Fatalf("after a standalone ack of another counter the handshake's outstanding messages are %v, want %d", got, counter)
	}
}
