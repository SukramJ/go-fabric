// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
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
	// The responder waits for the acknowledgement as it waits for a
	// message: the acknowledgement is dispatched at once, not after the
	// session's turn on the ReceiveInit ran out ([responderTurnBound]).
	ackedAt := time.Now()
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
	if d := time.Since(ackedAt); d > 700*time.Millisecond {
		t.Fatalf("Block 0 left %v after the ReceiveAccept's acknowledgement, want at once", d)
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

// TestWaitExchangeFreeEndsOnGiveUpAndExchangeEnd (review II, phase 7,
// decision 283): a wait for an exchange's message that is given up ends
// with mrp.ErrMaxRetransmissionsReached, not as a free slot, and a wait
// whose exchange ends ends with errExchangeEnded — the slot freed in the
// same instant included. matter.js's give-up rejects the ack wait and
// leaves #sentMessageToAck set, so no further message goes out
// (packages/protocol/src/protocol/MessageExchange.ts:973-978, :701-705 at
// db021da0).
func TestWaitExchangeFreeEndsOnGiveUpAndExchangeEnd(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	wait := func(tr *outboundReliableTracker, done chan struct{}) chan error {
		out := make(chan error, 1)
		blocked := make(chan struct{})
		go func() {
			out <- tr.waitExchangeFree(context.Background(), 7, 0x10, exchangeResponder, done, func() { close(blocked) })
		}()
		select {
		case <-blocked:
		case err := <-out:
			t.Fatalf("the wait returned (%v) while the message waits for its acknowledgement", err)
		}
		return out
	}
	result := func(out chan error) error {
		t.Helper()
		select {
		case err := <-out:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("the wait did not end")
			return nil
		}
	}

	tr := newOutboundReliableTracker(nil, clock.System{})
	if err := tr.trackExchangeMessage(1, 7, 0x10, false, []byte{1}, dest, now, 0); err != nil {
		t.Fatal(err)
	}
	out := wait(tr, make(chan struct{}))
	tr.mu.Lock()
	for _, e := range tr.pending {
		e.retries = mrp.MaxRetransmissions
	}
	tr.mu.Unlock()
	tr.Tick(now.Add(time.Hour), func(net.Addr, []byte) error { return nil })
	if err := result(out); !errors.Is(err, mrp.ErrMaxRetransmissionsReached) {
		t.Fatalf("a wait whose message was given up: %v, want ErrMaxRetransmissionsReached", err)
	}

	tr = newOutboundReliableTracker(nil, clock.System{})
	if err := tr.trackExchangeMessage(2, 7, 0x10, false, []byte{2}, dest, now, 0); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	out = wait(tr, done)
	close(done)
	if err := result(out); !errors.Is(err, errExchangeEnded) {
		t.Fatalf("a wait whose exchange ended: %v, want errExchangeEnded", err)
	}

	// The slot freed and the exchange ended together — a session drop
	// that ends its exchanges: the end wins.
	tr = newOutboundReliableTracker(nil, clock.System{})
	if err := tr.trackExchangeMessage(3, 7, 0x10, false, []byte{3}, dest, now, 0); err != nil {
		t.Fatal(err)
	}
	done = make(chan struct{})
	close(done)
	tr.DropSession(7)
	if err := tr.waitExchangeFree(context.Background(), 7, 0x10, exchangeResponder, done, nil); !errors.Is(err, errExchangeEnded) {
		t.Fatalf("a wait on a free slot of an ended exchange: %v, want errExchangeEnded", err)
	}
}

// TestBDXSendAfterAGiveUpSendsNothing (review II, phase 7, decision 283):
// a Send that waits while the exchange's previous message is given up
// returns an error and puts nothing on the wire.
func TestBDXSendAfterAGiveUpSendsNothing(t *testing.T) {
	t.Parallel()
	dev, _, got := newReliableBDXHarness(t, nil)
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
	if m, ok := nextBDX(got, 5*time.Second); !ok || m.opcode != bdx.MessageSendInit {
		t.Fatalf("the peer got %v (ok %t), want SendInit", m.opcode, ok)
	}
	second := make(chan error, 1)
	go func() { second <- ex.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockQuery), []byte{0x02}) }()
	time.Sleep(200 * time.Millisecond)
	tr := dev.br.outboundReliable
	tr.mu.Lock()
	for _, e := range tr.pending {
		e.retries = mrp.MaxRetransmissions
		e.nextSendAt = time.Time{}
	}
	tr.mu.Unlock()
	dev.br.tickOutboundReliable(tr, time.Now())
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("the Send waiting on the given-up SendInit returned nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Send waiting on the given-up SendInit did not end")
	}
	for {
		m, ok := nextBDX(got, 400*time.Millisecond)
		if !ok {
			break
		}
		if m.opcode != bdx.MessageSendInit {
			t.Fatalf("after the give-up the device sent %v", m.opcode)
		}
	}
}

// TestBDXSendAfterAGiveUpBeforeTheExchangeEndsSendsNothing (review II,
// phase 7, decision 283; Fable's read R2-1): a Send that begins after the
// tracker gave up the exchange's last message and before the exchange is
// ended — there is nothing to wait on — returns
// mrp.ErrMaxRetransmissionsReached and puts nothing on the wire, as
// matter.js's exchange, whose #sentMessageToAck stays set after the
// give-up, throws on a further send
// (packages/protocol/src/protocol/MessageExchange.ts:973-978, :701-705 at
// db021da0).
func TestBDXSendAfterAGiveUpBeforeTheExchangeEndsSendsNothing(t *testing.T) {
	t.Parallel()
	dev, _, got := newReliableBDXHarness(t, nil)
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
	if m, ok := nextBDX(got, 5*time.Second); !ok || m.opcode != bdx.MessageSendInit {
		t.Fatalf("the peer got %v (ok %t), want SendInit", m.opcode, ok)
	}
	// The tracker's give-up alone: its result is not handed to the bridge,
	// so the exchange is not ended — the window between the two.
	tr := dev.br.outboundReliable
	tr.mu.Lock()
	for _, e := range tr.pending {
		e.retries = mrp.MaxRetransmissions
		e.nextSendAt = time.Time{}
	}
	tr.mu.Unlock()
	tr.Tick(time.Now(), func(net.Addr, []byte) error { return nil })
	if ex.(*initiatedSecureExchange).ended() {
		t.Fatal("the exchange ended; the test needs the window before its end")
	}
	if err := ex.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageBlockQuery), []byte{0x02}); !errors.Is(err, mrp.ErrMaxRetransmissionsReached) {
		t.Fatalf("a Send after the give-up: %v, want ErrMaxRetransmissionsReached", err)
	}
	for {
		m, ok := nextBDX(got, 400*time.Millisecond)
		if !ok {
			break
		}
		if m.opcode != bdx.MessageSendInit {
			t.Fatalf("after the give-up the device sent %v", m.opcode)
		}
	}
}
