// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// recorder collects the hook calls of one exchange.
type hookRecorder struct {
	mu       sync.Mutex
	sends    []int
	receipts int
	received []receipt
}

// receipt is one receive-notifier call: the message's opcode and counter
// and the duplicate flag.
type receipt struct {
	opcode    uint8
	counter   uint32
	duplicate bool
}

func (r *hookRecorder) onSend(n int) {
	r.mu.Lock()
	r.sends = append(r.sends, n)
	r.mu.Unlock()
}

func (r *hookRecorder) onReceive(msg *Message, duplicate bool) {
	r.mu.Lock()
	r.receipts++
	r.received = append(r.received, receipt{opcode: msg.Opcode, counter: msg.Counter, duplicate: duplicate})
	r.mu.Unlock()
}

func (r *hookRecorder) receivedSnapshot() []receipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.received)
}

func (r *hookRecorder) snapshot() (sends []int, receipts int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sends), r.receipts
}

// TestOnSendAndOnReceive: OnSend sees 0 for a first transmission and the
// retransmission number after (MessageExchange.ts:768, :943); OnReceive
// sees every message the exchange takes, the ack included
// (MessageExchange.ts:499 at db021da0).
func TestOnSendAndOnReceive(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0311, false)
	var rec hookRecorder
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{OnSend: rec.onSend, OnReceive: rec.onReceive})
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	first := p.decode(t, conn.nextOn(t, x).bytes)
	iv, active := s.timing()
	clock.Advance(RetransmissionInterval(0, iv, active, 0.5))
	conn.next(t)
	_ = x.HasUnackedMessage()
	reply := message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: first.hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}
	p.send(t, s.ID(), reply, nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	clock.Advance(StandaloneAckTimeout) // the standalone ack is a first transmission too
	conn.next(t)
	sends, receipts := rec.snapshot()
	if !slices.Equal(sends, []int{0, 1, 0}) || receipts != 1 {
		t.Fatalf("OnSend %v, OnReceive %d; want [0 1 0], 1", sends, receipts)
	}
}

// TestReceiveNotifierSeesEveryMessage: the receive notifier is called with
// every message the exchange takes — a standalone ack, a reliable message
// and its duplicate — with the duplicate flag onMessageReceived takes
// (MessageExchange.ts:475 at db021da0), on the receive path: it has run
// when the datagram's delivery returns (the call at :499 precedes the
// acknowledgement bookkeeping).
func TestReceiveNotifierSeesEveryMessage(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0312, false)
	var rec hookRecorder
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{OnReceive: rec.onReceive})
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	first := p.decode(t, conn.nextOn(t, x).bytes)

	ack := message.ProtocolHeader{HasAck: true, AckCounter: first.hdr.MessageCounter, Opcode: mrp.StandaloneAckOpcode, ExchangeID: x.ID(), ProtocolID: mrp.SecureChannelProtocolID}
	p.send(t, s.ID(), ack, nil)
	if got := rec.receivedSnapshot(); len(got) != 1 || got[0].opcode != mrp.StandaloneAckOpcode || got[0].duplicate {
		t.Fatalf("after the standalone ack: %+v", got)
	}
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}

	report := message.ProtocolHeader{NeedsAck: true, Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}
	b := p.send(t, s.ID(), report, []byte{0x15, 0x18})
	m.HandleDatagram(b, peerAddr) // the same counter again: a duplicate
	got := rec.receivedSnapshot()
	if len(got) != 3 {
		t.Fatalf("notifier calls %+v, want 3", got)
	}
	if got[1].opcode != im.OpcodeReportData || got[1].duplicate {
		t.Fatalf("second call %+v, want the report, not a duplicate", got[1])
	}
	if got[2].opcode != im.OpcodeReportData || !got[2].duplicate || got[2].counter != got[1].counter {
		t.Fatalf("third call %+v, want the report again as a duplicate of %+v", got[2], got[1])
	}
}

// TestSetReceiveNotifierOnAPeerOpenedExchange: an exchange the peer opened
// is accepted without options; SetReceiveNotifier installs its notifier,
// which sees the messages that follow, and a second call leaves the first
// notifier in place — matter.js MessageExchange's onReceive setter
// (MessageExchange.ts:385-389 at db021da0).
func TestSetReceiveNotifierOnAPeerOpenedExchange(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0313, false)
	var rec, other hookRecorder
	installed := make(chan *Exchange, 1)
	release := make(chan struct{})
	defer close(release)
	m.SetProtocolHandler(im.InteractionModelProtocolID, func(x *Exchange, _ *Message) {
		x.SetReceiveNotifier(rec.onReceive)
		x.SetReceiveNotifier(other.onReceive)
		installed <- x
		<-release
	})
	p.send(t, s.ID(), peerOpens(0x6101, im.InteractionModelProtocolID, im.OpcodeReadRequest, true), []byte{0x15, 0x18})
	select {
	case <-installed:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called")
	}
	p.send(t, s.ID(), peerOpens(0x6101, im.InteractionModelProtocolID, im.OpcodeStatusResponse, true), []byte{0x15, 0x18})
	if got := rec.receivedSnapshot(); len(got) != 1 || got[0].opcode != im.OpcodeStatusResponse || got[0].duplicate {
		t.Fatalf("installed notifier saw %+v, want the status response", got)
	}
	if got := other.receivedSnapshot(); len(got) != 0 {
		t.Fatalf("second notifier saw %+v, want nothing: the first stays", got)
	}
}
