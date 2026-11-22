// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestInitiatedExchangeIgnoresPeerInitiatedCollision holds the direction
// check of the three hooks that hand a message to an exchange before the
// ordinary dispatch (R2-P6-test-0801): an exchange is named by its id and
// by who opened it, so a message whose I flag says the other side opened
// it is not the held exchange's, even on the same session and exchange id
// — the hook leaves it to the ordinary dispatch, and the exchange receives
// nothing. Likewise a secure exchange takes nothing from the unsecured
// session, and a handshake nothing from a secure one.
func TestInitiatedExchangeIgnoresPeerInitiatedCollision(t *testing.T) {
	r := newExchangeSessionRig(t)
	read := func(sessionID uint16, initiator bool, exchange uint16) (*message.Header, message.ProtocolHeader, []byte) {
		return &message.Header{SessionID: sessionID, MessageCounter: 99},
			message.ProtocolHeader{Initiator: initiator, ExchangeID: exchange, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest},
			[]byte{0x15, 0x18}
	}

	// The peer's BDX exchange (the peer opened it): a message on its id
	// without the I flag, or on the unsecured session, is not its.
	for _, c := range []struct {
		name      string
		sessionID uint16
		initiator bool
	}{{"responder direction", r.sid, false}, {"unsecured session", 0, true}} {
		hdr, proto, payload := read(c.sessionID, c.initiator, r.peerXID)
		if r.dev.br.deliverRespondedBDX(context.Background(), hdr, proto, payload) {
			t.Errorf("BDX responder exchange, %s: the hook took the message", c.name)
		}
	}
	select {
	case msg := <-r.responder.got:
		t.Errorf("the peer's BDX exchange received protocol %#x opcode %#x", msg.ProtocolID, msg.Opcode)
	case err := <-r.responder.ended:
		t.Errorf("the peer's BDX exchange ended: %v", err)
	default: // the hooks deliver before they return: nothing to wait for
	}

	// The exchange this node opened: a message on its id with the I flag
	// set, or on the unsecured session, is not its.
	nodeEx, ok := r.nodeEx.(*initiatedSecureExchange)
	if !ok {
		t.Fatalf("node exchange is %T", r.nodeEx)
	}
	for _, c := range []struct {
		name      string
		sessionID uint16
		initiator bool
	}{{"initiator direction", r.sid, true}, {"unsecured session", 0, false}} {
		if r.dev.br.deliverInitiatedSecure(read(c.sessionID, c.initiator, nodeEx.exchangeID)) {
			t.Errorf("node-opened exchange, %s: the hook took the message", c.name)
		}
	}
	// The hooks deliver before they return: the queue is read directly. A
	// Receive with a deadline would report the silent peer lost
	// (initiatedSecureExchange.Receive) and end the control below.
	if n := len(nodeEx.in); n != 0 {
		t.Errorf("the node-opened exchange received %d messages", n)
	}

	// The control: each exchange takes the message of its own direction.
	if hdr, proto, payload := read(r.sid, false, nodeEx.exchangeID); !r.dev.br.deliverInitiatedSecure(hdr, proto, payload) {
		t.Error("the node-opened exchange refused the peer's response")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := r.nodeEx.Receive(ctx2); err != nil {
		t.Errorf("the node-opened exchange's response: %v", err)
	}
	if hdr, proto, payload := read(r.sid, true, r.peerXID); !r.dev.br.deliverRespondedBDX(context.Background(), hdr, proto, payload) {
		t.Error("the peer's BDX exchange refused the peer's message")
	}
	select {
	case <-r.responder.got:
	case err := <-r.responder.ended:
		t.Errorf("the peer's BDX exchange ended: %v", err)
	case <-ctx2.Done():
		t.Error("the peer's BDX exchange did not receive the peer's message")
	}
}

// TestInitiatedHandshakeIgnoresPeerInitiatedCollision is the CASE
// initiator's share of R2-P6-test-0801: a message on the handshake's
// exchange id and initiator node id is not the handshake's when its I flag
// says the peer opened the exchange, or when it comes on a secure session.
func TestInitiatedHandshakeIgnoresPeerInitiatedCollision(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	h := startInitiatedHandshake(t, r)
	for _, c := range []struct {
		name      string
		sessionID uint16
		initiator bool
	}{{"initiator direction", 0, true}, {"secure session", 0x0101, false}} {
		hdr := &message.Header{SessionID: c.sessionID, MessageCounter: 77, DestSize: message.DestNodeID, DestNodeID: h.sigma1.SourceNodeID}
		proto := message.ProtocolHeader{
			Opcode: mrp.SCOpcodeSigma2, ExchangeID: h.exchange.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
			Initiator: c.initiator,
		}
		if r.b.deliverInitiatedSecureChannel(hdr, proto, []byte{0xFF}) {
			t.Errorf("%s: the hook took the message", c.name)
		}
	}
	select {
	case err := <-h.done:
		t.Fatalf("the handshake ended on a message that was not its: %v", err)
	default: // the hook hands over before it returns: nothing to wait for
	}
}
