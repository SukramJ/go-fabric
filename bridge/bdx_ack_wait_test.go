// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// bdxSeen is one BDX message the device sent, with its message counter.
type bdxSeen struct {
	counter uint32
	opcode  bdx.MessageType
	payload []byte
}

// newReliableBDXHarness is [newBDXSessionHarness] with the MRP trackers
// attached before Start, so the device's BDX messages are reliable and
// retransmitted by the pump; the fake peer acknowledges nothing by itself.
// It returns the device too.
func newReliableBDXHarness(t *testing.T, responder BDXResponder) (*resumeDevice, *fakeController, chan bdxSeen) {
	t.Helper()
	fab := newResumeFabric(t)
	peer := newFakeController(t, fab)
	got := make(chan bdxSeen, 64)
	peer.mu.Lock()
	peer.secureHook = func(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
		if proto.ProtocolID == bdx.ProtocolID {
			got <- bdxSeen{counter: hdr.MessageCounter, opcode: bdx.MessageType(proto.Opcode), payload: append([]byte(nil), payload...)}
			return true
		}
		return false
	}
	peer.mu.Unlock()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: peer.addr})
		if responder != nil {
			br.AttachBDXResponder(responder)
		}
	})
	peer.setBridge(dev.addr(t))
	return dev, peer, got
}

// nextBDX returns the next BDX message the device sent within d.
func nextBDX(got chan bdxSeen, d time.Duration) (bdxSeen, bool) {
	select {
	case m := <-got:
		return m, true
	case <-time.After(d):
		return bdxSeen{}, false
	}
}

// sendBDXAck sends a BDX message on exchange id, the peer in role
// initiator, acknowledging ack when it is non-zero.
func sendBDXAck(peer *fakeController, exchangeID uint16, initiator bool, ack uint32, opcode uint8, payload []byte) {
	peer.sendSecureAck(message.ProtocolHeader{Initiator: !initiator, ExchangeID: exchangeID, ProtocolID: bdx.ProtocolID}, ack, opcode, payload)
}

// TestBDXBlockWaitsForTheAcceptsAck (review II, phase 7, decision 276): an
// OTA requestor's ReceiveInit answered as the driving sender — the
// ReceiveAccept, then the first Block with no message of the peer between.
// The Block is not sent before the ReceiveAccept is acknowledged, and a
// ReceiveAccept the peer did not acknowledge is retransmitted; before, the
// Block took the ReceiveAccept's place in the retransmit table and a lost
// ReceiveAccept was never sent again. matter.js's send does not return
// before its acknowledgement (packages/protocol/src/protocol/MessageExchange.ts:786,
// :822-830 at db021da0), and its sender-drive responder sends the first
// Block after the ReceiveAccept's send completed
// (bdx/bdx-session-initiator.ts:100, bdx/flow/DrivenSendingFlow.ts:33).
// On the transfer's exchange the receive rules hold as on any other: a
// BlockAck that does not acknowledge the Block, or names another counter,
// is refused and drives nothing (MessageExchange.ts:521-534).
func TestBDXBlockWaitsForTheAcceptsAck(t *testing.T) {
	t.Parallel()
	image := bytes.Repeat([]byte{0xA5}, 200)
	responder := bdx.NewResponder(nil)
	dev, peer, got := newReliableBDXHarness(t, responder)
	responder.EnablePeerForScope(bdx.Peer{FabricIndex: resumeFabricIndex, NodeID: resumeCtrlNode}, "ota", memStorage{"image.bin": image},
		bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := dev.br.ConnectOtaProvider(ctx, resumeFabricIndex, resumeCtrlNode); err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}

	const x = 0x4343
	init, err := bdx.Init{
		TransferControl: bdx.TransferControl{SenderDrive: true},
		MaxBlockSize:    64,
		FileDesignator:  []byte("ota/image.bin"),
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sendBDXAck(peer, x, true, 0, uint8(bdx.MessageReceiveInit), init)
	accept, ok := nextBDX(got, 5*time.Second)
	if !ok || accept.opcode != bdx.MessageReceiveAccept {
		t.Fatalf("first answer %v (ok %t), want ReceiveAccept", accept.opcode, ok)
	}
	// Unacknowledged, the ReceiveAccept is retransmitted and no Block goes
	// out.
	for {
		m, ok := nextBDX(got, 8*time.Second)
		if !ok {
			t.Fatal("the unacknowledged ReceiveAccept was not retransmitted")
		}
		if m.opcode != bdx.MessageReceiveAccept {
			t.Fatalf("the device sent %v before the ReceiveAccept was acknowledged", m.opcode)
		}
		if m.counter == accept.counter {
			break
		}
	}
	peer.sendSecureAck(message.ProtocolHeader{Initiator: false, ExchangeID: x, ProtocolID: mrp.SecureChannelProtocolID}, accept.counter, mrp.StandaloneAckOpcode, nil)
	var block0 bdxSeen
	for {
		m, ok := nextBDX(got, 5*time.Second)
		if !ok {
			t.Fatal("no Block after the ReceiveAccept was acknowledged")
		}
		if m.opcode == bdx.MessageReceiveAccept {
			continue // a retransmission already on its way
		}
		block0 = m
		break
	}
	if blk, err := bdx.DecodeBlock(block0.payload); block0.opcode != bdx.MessageBlock || err != nil || blk.BlockCounter != 0 {
		t.Fatalf("after the acknowledgement the device sent %v %+v (%v), want Block 0", block0.opcode, blk, err)
	}

	blockAck := bdx.CounterOnly{BlockCounter: 0}.Encode()
	// noBlock1 fails t when the device sends anything but Block 0's
	// retransmission within 400ms.
	noBlock1 := func(what string) {
		t.Helper()
		deadline := time.Now().Add(400 * time.Millisecond)
		for {
			m, ok := nextBDX(got, time.Until(deadline))
			if !ok {
				return
			}
			if m.counter != block0.counter {
				t.Fatalf("%s drove the transfer: the device sent %v", what, m.opcode)
			}
		}
	}
	sendBDXAck(peer, x, true, 0, uint8(bdx.MessageBlockAck), blockAck)
	noBlock1("a BlockAck without the Block's acknowledgement")
	sendBDXAck(peer, x, true, block0.counter+1, uint8(bdx.MessageBlockAck), blockAck)
	noBlock1("a BlockAck acknowledging another counter")
	sendBDXAck(peer, x, true, block0.counter, uint8(bdx.MessageBlockAck), blockAck)
	for {
		m, ok := nextBDX(got, 5*time.Second)
		if !ok {
			t.Fatal("no Block 1 after the BlockAck that acknowledges Block 0")
		}
		if m.counter == block0.counter {
			continue
		}
		if blk, err := bdx.DecodeBlock(m.payload); err != nil || blk.BlockCounter != 1 {
			t.Fatalf("after Block 0's BlockAck the device sent %v %+v (%v), want Block 1", m.opcode, blk, err)
		}
		break
	}
}

// TestBDXInitiatedExchangeWaitsAndRefuses (review II, phase 7, decision
// 276): on a BDX exchange this node opened, a second Send waits until the
// first message is acknowledged, and the peer's answer that does not
// acknowledge it — no acknowledgement, or another counter — is refused on
// receive and never reaches the exchange; the answer that acknowledges it
// does, and frees the waiting Send. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:822-830 (the send
// returns with its acknowledgement) and :521-534 at db021da0.
func TestBDXInitiatedExchangeWaitsAndRefuses(t *testing.T) {
	t.Parallel()
	dev, peer, got := newReliableBDXHarness(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := dev.br.ConnectOtaProvider(ctx, resumeFabricIndex, resumeCtrlNode)
	if err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}
	ex, err := s.OpenExchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ex.Close() }()
	if err := ex.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageSendInit), []byte{0x01}); err != nil {
		t.Fatalf("SendInit: %v", err)
	}
	sendInit, ok := nextBDX(got, 5*time.Second)
	if !ok || sendInit.opcode != bdx.MessageSendInit {
		t.Fatalf("the peer got %v (ok %t), want SendInit", sendInit.opcode, ok)
	}
	xid := ex.(*initiatedSecureExchange).exchangeID

	second := make(chan error, 1)
	go func() { second <- ex.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockQuery), []byte{0x02}) }()
	select {
	case err := <-second:
		t.Fatalf("a second Send returned (%v) while the SendInit waits for its acknowledgement", err)
	case <-time.After(300 * time.Millisecond):
	}

	receive := func() (bdx.Message, error) {
		t.Helper()
		rctx, rcancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer rcancel()
		return ex.Receive(rctx)
	}
	sendBDXAck(peer, xid, false, 0, uint8(bdx.MessageSendAccept), []byte{0x03})
	if m, err := receive(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a SendAccept without the SendInit's acknowledgement reached the exchange: %+v, %v", m, err)
	}
	sendBDXAck(peer, xid, false, sendInit.counter+1, uint8(bdx.MessageSendAccept), []byte{0x03})
	if m, err := receive(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a SendAccept acknowledging another counter reached the exchange: %+v, %v", m, err)
	}
	select {
	case err := <-second:
		t.Fatalf("the second Send returned (%v) on a refused answer", err)
	default:
	}
	sendBDXAck(peer, xid, false, sendInit.counter, uint8(bdx.MessageSendAccept), []byte{0x03})
	if m, err := receive(); err != nil || bdx.MessageType(m.Opcode) != bdx.MessageSendAccept {
		t.Fatalf("the acknowledging SendAccept: %+v, %v", m, err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("the second Send: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second Send still waits after the SendInit was acknowledged")
	}
}
