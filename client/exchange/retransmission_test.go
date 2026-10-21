// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
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
