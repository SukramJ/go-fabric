// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestRetransmissionCountAndRestartSaving: the count follows the
// retransmissions of the pending message and resets with its
// acknowledgement (MessageExchange.ts:263, :508, :886); the restart saving
// is the live timer's interval less the base backoff, 0 before any timer
// (:1227-1235).
func TestRetransmissionCountAndRestartSaving(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0301, false)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	if got := x.RetransmissionRestartSaving(); got != 0 {
		t.Fatalf("saving before any send = %v, want 0", got)
	}
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	first := p.decode(t, conn.nextOn(t, x).bytes)
	iv, active := s.timing()
	base := RetransmissionInterval(0, iv, active, 0.5)
	if n, saving := x.RetransmissionCount(), x.RetransmissionRestartSaving(); n != 0 || saving != 0 {
		t.Fatalf("after the first transmission count %d saving %v, want 0, 0", n, saving)
	}
	clock.Advance(base)
	conn.next(t)
	_ = x.HasUnackedMessage()
	if n := x.RetransmissionCount(); n != 1 {
		t.Fatalf("count after one retransmission = %d", n)
	}
	iv, active = s.timing()
	want := RetransmissionInterval(1, iv, active, 0.5) - RetransmissionInterval(0, iv, active, 0.5)
	if got := x.RetransmissionRestartSaving(); got != want {
		t.Fatalf("saving after one retransmission = %v, want %v", got, want)
	}
	clock.Advance(RetransmissionInterval(1, iv, active, 0.5))
	conn.next(t)
	_ = x.HasUnackedMessage()
	if n := x.RetransmissionCount(); n != 2 {
		t.Fatalf("count after two retransmissions = %d", n)
	}
	ackProto := ackFor(x.ID(), first.hdr.MessageCounter)
	p.send(t, s.ID(), ackProto, nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	if n := x.RetransmissionCount(); n != 0 {
		t.Fatalf("count after the ack = %d, want 0", n)
	}
}

// ackFor is the peer's standalone ack of our message counter on exchange xid.
func ackFor(xid uint16, counter uint32) message.ProtocolHeader {
	return message.ProtocolHeader{HasAck: true, AckCounter: counter, Opcode: mrp.StandaloneAckOpcode, ExchangeID: xid, ProtocolID: mrp.SecureChannelProtocolID}
}

// TestCancelledSendEndsRetransmission (W4-ctlnet-03): a Send whose ctx
// ends while it waits for the ack ends the message's ack wait, so a
// retransmission already past its unlock — modelled by calling retransmit
// after the cancel — neither sends nor re-arms. Mirrors matter.js
// MessageExchange.ts:812-818 (#endAckWait on abort).
func TestCancelledSendEndsRetransmission(t *testing.T) {
	t.Parallel()
	e := mxNew(t, false, nil)
	x := e.exchange(t, im.InteractionModelProtocolID)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- x.Send(ctx, im.OpcodeReadRequest, nil, SendOptions{}) }()
	e.conn.waitSent(t, 1)
	x.HasUnackedMessage() // the timer is armed once this returns
	x.mu.Lock()
	p := x.sentToAck
	x.mu.Unlock()
	cancel()
	if err := waitErr(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send error %v, want context.Canceled", err)
	}
	if x.retransmit(p, 0) {
		t.Fatal("a retransmission ran after the cancelled Send")
	}
	x.mu.Lock()
	armed := x.retransArmed
	x.mu.Unlock()
	if armed || len(e.conn.datagrams()) != 1 {
		t.Fatalf("retransmission armed %v, %d datagrams sent, want 1", armed, len(e.conn.datagrams()))
	}
}

// TestClosingResponderKeepsRetransmitting (W4-ctlnet-06): a closing
// responder exchange (one the peer opened) still retransmits its pending
// message; a closing initiator exchange stops. Mirrors matter.js
// MessageExchange.ts:907-910 with considerClosed (:390-392).
func TestClosingResponderKeepsRetransmitting(t *testing.T) {
	t.Parallel()
	for _, initiator := range []bool{false, true} {
		e := mxNew(t, false, nil)
		var x *Exchange
		if initiator {
			x = e.exchange(t, im.InteractionModelProtocolID)
		} else {
			x = newExchange(e.m, e.s, 7, im.InteractionModelProtocolID, false, Options{})
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = x.Send(ctx, im.OpcodeReportData, nil, SendOptions{}) }()
		e.conn.waitSent(t, 1)
		x.HasUnackedMessage()
		x.mu.Lock()
		p := x.sentToAck
		x.closing = true
		x.mu.Unlock()
		if got := x.retransmit(p, 0); got == initiator {
			t.Errorf("initiator %v: retransmitted %v while closing", initiator, got)
		}
		cancel()
	}
}
