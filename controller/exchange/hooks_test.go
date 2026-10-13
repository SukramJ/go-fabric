// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// recorder collects the hook calls of one exchange.
type hookRecorder struct {
	mu       sync.Mutex
	sends    []int
	receipts int
}

func (r *hookRecorder) onSend(n int) {
	r.mu.Lock()
	r.sends = append(r.sends, n)
	r.mu.Unlock()
}

func (r *hookRecorder) onReceive() {
	r.mu.Lock()
	r.receipts++
	r.mu.Unlock()
}

func (r *hookRecorder) snapshot() ([]int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sends), r.receipts
}

// TestOnSendAndOnReceive: OnSend sees 0 for a first transmission and the
// retransmission number after (MessageExchange.ts:768, :943); OnReceive
// sees every message the exchange takes, the ack included (:469).
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

// TestHooksAdoptedFromContext: an exchange takes the hooks of the context
// of its first Send, calls Exchange once, and keeps a hook its Options
// set (MessageExchange.ts:351-369).
func TestHooksAdoptedFromContext(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0312, false)
	var viaCtx, viaOpts hookRecorder
	var adopted []*Exchange
	ctx := WithHooks(context.Background(), Hooks{
		Exchange:  func(x *Exchange) { adopted = append(adopted, x) },
		OnSend:    viaCtx.onSend,
		OnReceive: viaCtx.onReceive,
	})
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{OnSend: viaOpts.onSend})
	done := make(chan error, 1)
	go func() { done <- x.Send(ctx, im.OpcodeReadRequest, nil, SendOptions{}) }()
	first := p.decode(t, conn.nextOn(t, x).bytes)
	reply := message.ProtocolHeader{HasAck: true, AckCounter: first.hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}
	p.send(t, s.ID(), reply, nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Next(ctx, ReceiveOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 1 || adopted[0] != x {
		t.Fatalf("Exchange hook called %d times", len(adopted))
	}
	ctxSends, ctxReceipts := viaCtx.snapshot()
	optSends, _ := viaOpts.snapshot()
	if len(ctxSends) != 0 || !slices.Equal(optSends, []int{0}) || ctxReceipts != 1 {
		t.Fatalf("context sends %v receipts %d, options sends %v", ctxSends, ctxReceipts, optSends)
	}
	// A context without hooks changes nothing.
	if _, err := x.Next(canceled(), ReceiveOptions{}); err == nil {
		t.Fatal("Next on a cancelled context succeeded")
	}
}

func canceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
