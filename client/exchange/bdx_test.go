// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/transport/message"
)

// TestBDXExchangeCarriesATransfer: an exchange opened on the BDX protocol
// carries a transfer's messages both ways as a bdx.Exchange — the send on
// BDX, the peer's answer to Receive and to the queue notifier first — and
// is not reliable on a UDP session.
func TestBDXExchangeCarriesATransfer(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0201, false)
	x, err := m.Initiate(s, bdx.ProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ex := NewBDXExchange(x)
	defer func() { _ = ex.Close() }()
	if ex.Reliable() {
		t.Error("an exchange on a UDP session reports reliable")
	}
	if ex.MaxPayloadSize() != s.MaxPayloadSize() {
		t.Errorf("MaxPayloadSize = %d, want the session's %d", ex.MaxPayloadSize(), s.MaxPayloadSize())
	}
	notified := make(chan bdx.Message, 1)
	ex.OnReceive(func(m bdx.Message) { notified <- m })

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- ex.Send(context.Background(), bdx.ProtocolID, uint8(bdx.MessageReceiveInit), []byte{0x01})
	}()
	out := p.decode(t, conn.next(t).bytes)
	if out.proto.ProtocolID != bdx.ProtocolID || out.proto.Opcode != uint8(bdx.MessageReceiveInit) || !out.proto.Initiator {
		t.Fatalf("sent %+v, want a BDX ReceiveInit opening the exchange", out.proto)
	}
	ack := out.hdr.MessageCounter
	p.send(t, s.ID(), message.ProtocolHeader{
		NeedsAck: true, HasAck: true, AckCounter: ack, Opcode: uint8(bdx.MessageReceiveAccept),
		ExchangeID: out.proto.ExchangeID, ProtocolID: bdx.ProtocolID,
	}, []byte{0x02})
	if err := waitErr(t, sendDone); err != nil {
		t.Fatalf("Send: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case m := <-notified:
		if m.Opcode != uint8(bdx.MessageReceiveAccept) {
			t.Errorf("notified %+v", m)
		}
	case <-ctx.Done():
		t.Fatal("the queue notifier saw nothing")
	}
	got, err := ex.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.ProtocolID != bdx.ProtocolID || got.Opcode != uint8(bdx.MessageReceiveAccept) || len(got.Payload) != 1 || got.Payload[0] != 0x02 {
		t.Errorf("received %+v", got)
	}
}
