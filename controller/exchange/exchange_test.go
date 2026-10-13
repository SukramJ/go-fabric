// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// newUnsecured opens an unsecured session and its peer.
func newUnsecured(t *testing.T, m *Manager) (*Session, *peer) {
	t.Helper()
	s, err := m.NewUnsecuredSession(peerAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, &peer{m: m, s: s, counter: 100}
}

// TestExchangeIDsAreConsecutiveAndIndexedApart: initiated exchanges take
// consecutive ids, and a message with the id of one of ours that the peer
// marks as initiator is not routed to it (it is a different exchange,
// ExchangeManager.ts:147, :333-335) but acknowledged as unsolicited.
func TestExchangeIDsAreConsecutiveAndIndexedApart(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	x1, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	x2, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	if x2.ID() != x1.ID()+1 {
		t.Fatalf("exchange ids %d, %d: want consecutive", x1.ID(), x2.ID())
	}
	if x1.Session() != s {
		t.Fatal("Session() is not the session the exchange was opened on")
	}
	// The peer opens its own exchange with x1's id: not ours.
	p.counter++
	hdr := message.Header{MessageCounter: p.counter, HasSourceNodeID: true, SourceNodeID: 0x5555}
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeStatusReport, ExchangeID: x1.ID(), ProtocolID: 0}
	m.HandleDatagram(append(hdr.Marshal(), proto.Marshal()...), peerAddr)
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.Initiator || !ack.proto.HasAck || ack.proto.AckCounter != p.counter ||
		ack.hdr.DestSize != message.DestNodeID || ack.hdr.DestNodeID != 0x5555 {
		t.Fatalf("unsolicited ack = %+v / %+v", ack.hdr, ack.proto)
	}
	// A reply on x1 (responder side) reaches it.
	p.reply(x1.ID(), 0, 0x21, false, nil, []byte{1})
	msg, err := x1.Next(context.Background(), ReceiveOptions{})
	if err != nil || msg.Opcode != 0x21 {
		t.Fatalf("Next = %+v, %v", msg, err)
	}
}

// TestUnsolicitedMessageWithoutAckIsDropped: a peer message no exchange
// of ours takes and that asks for no ack is dropped without an answer;
// one on our unsecured session that asks for an ack is acknowledged.
func TestUnsolicitedMessageWithoutAckIsDropped(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	_, p := newUnsecured(t, m)
	p.reply(0x4242, 0, 0x40, false, nil, nil)
	conn.none(t)
	c := p.reply(0x4242, 0, 0x40, true, nil, nil)
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.AckCounter != c || !ack.proto.Initiator || !ack.hdr.HasSourceNodeID || ack.hdr.SourceNodeID != p.s.NodeID() {
		t.Fatalf("ack = %+v / %+v", ack.hdr, ack.proto)
	}
	// An unsecured response for a session we do not have, and a secure
	// message for an unknown session, are dropped.
	hdr := message.Header{MessageCounter: 1, DestSize: message.DestNodeID, DestNodeID: 0x9999}
	m.HandleDatagram(append(hdr.Marshal(), message.ProtocolHeader{NeedsAck: true}.Marshal()...), peerAddr)
	hdr = message.Header{SessionID: 0x3333, MessageCounter: 1}
	m.HandleDatagram(append(hdr.Marshal(), 0, 0, 0, 0), peerAddr)
	m.HandleDatagram([]byte{0xFF}, peerAddr)
	group := message.Header{SessionType: message.SessionGroup, DestSize: message.DestGroup, DestGroupID: 1, MessageCounter: 1}
	m.HandleDatagram(group.Marshal(), peerAddr)
	conn.none(t)
}

// TestSendWaitsForAckAndPiggybacksOwedAck: Send returns once the peer
// acknowledged; the acknowledgement owed for a received message rides on
// the next send (MessageExchange.ts:679-686) and no standalone ack goes out.
func TestSendWaitsForAckAndPiggybacksOwedAck(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, mrp.SCOpcodePBKDFParamRequest, []byte{1, 2}, SendOptions{})
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	if !first.proto.Initiator || !first.proto.NeedsAck || first.proto.HasAck || first.proto.ExchangeID != x.ID() ||
		!first.hdr.HasSourceNodeID || first.hdr.SourceNodeID != s.NodeID() || first.hdr.SessionID != 0 {
		t.Fatalf("first message %+v / %+v", first.hdr, first.proto)
	}
	if !x.HasUnackedMessage() {
		t.Fatal("HasUnackedMessage false while waiting for the ack")
	}
	// The response acknowledges and itself asks for an ack.
	c := p.reply(x.ID(), 0, mrp.SCOpcodePBKDFParamResponse, true, u32(first.hdr.MessageCounter), []byte{3})
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if x.HasUnackedMessage() {
		t.Fatal("HasUnackedMessage true after the ack")
	}
	msg, err := x.Next(context.Background(), ReceiveOptions{})
	if err != nil || msg.Opcode != mrp.SCOpcodePBKDFParamResponse || string(msg.Payload) != "\x03" {
		t.Fatalf("Next = %+v, %v", msg, err)
	}
	clock.Advance(100 * time.Millisecond)
	conn.none(t)
	done = sendAsync(x, mrp.SCOpcodePake1, nil, SendOptions{})
	second := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	if !second.proto.HasAck || second.proto.AckCounter != c {
		t.Fatalf("second message does not piggyback the ack of %d: %+v", c, second.proto)
	}
	// The owed ack's timer was stopped: no standalone ack follows.
	clock.Advance(StandaloneAckTimeout)
	conn.none(t)
	p.reply(x.ID(), 0, mrp.StandaloneAckOpcode, false, u32(second.hdr.MessageCounter), nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
}

// TestSecondSendWhileUnackedIsFlowError: MessageExchange.ts:669-673.
func TestSecondSendWhileUnackedIsFlowError(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, nil, SendOptions{})
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	if err := x.Send(context.Background(), 0x22, nil, SendOptions{}); !errors.Is(err, ErrFlow) {
		t.Fatalf("second Send = %v, want ErrFlow", err)
	}
	p.reply(x.ID(), 0, mrp.StandaloneAckOpcode, false, u32(first.hdr.MessageCounter), nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
}

// TestDuplicateIsAckedAtOnceAndNotDelivered: a retransmitted message is
// acknowledged with a standalone ack straight away and not queued again;
// a single message gets its standalone ack after 200 ms (MRP.ts:48).
func TestDuplicateIsAckedAtOnceAndNotDelivered(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, nil, SendOptions{})
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	c := p.reply(x.ID(), 0, 0x21, true, u32(first.hdr.MessageCounter), []byte{9})
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	clock.Advance(StandaloneAckTimeout - time.Millisecond)
	conn.none(t)
	p.replyWith(c, x.ID(), 0, 0x21, true, u32(first.hdr.MessageCounter), []byte{9})
	dup := decodeUnsecured(t, conn.next(t).bytes)
	if dup.proto.Opcode != mrp.StandaloneAckOpcode || dup.proto.AckCounter != c || dup.proto.NeedsAck || !dup.proto.Initiator {
		t.Fatalf("duplicate ack %+v", dup.proto)
	}
	clock.Advance(time.Millisecond)
	timed := decodeUnsecured(t, conn.next(t).bytes)
	if timed.proto.Opcode != mrp.StandaloneAckOpcode || timed.proto.AckCounter != c {
		t.Fatalf("timed ack %+v", timed.proto)
	}
	if _, err := x.Next(context.Background(), ReceiveOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := x.Next(ctx, ReceiveOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Next = %v, want the duplicate dropped", err)
	}
}

// TestSecondReliableMessageAcksTheFirstAtOnce: an owed ack still pending
// when the next reliable message arrives goes out at once
// (MessageExchange.ts:523-531).
func TestSecondReliableMessageAcksTheFirstAtOnce(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	c1 := p.reply(x.ID(), im.InteractionModelProtocolID, im.OpcodeReportData, true, u32(first.hdr.MessageCounter), nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	p.reply(x.ID(), im.InteractionModelProtocolID, im.OpcodeReportData, true, nil, nil)
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.AckCounter != c1 || ack.proto.ProtocolID != mrp.SecureChannelProtocolID {
		t.Fatalf("ack %+v", ack.proto)
	}
	// A message of a foreign protocol is dropped.
	p.reply(x.ID(), 0x0002, 0x01, false, nil, nil)
	for range 2 {
		if _, err := x.Next(context.Background(), ReceiveOptions{Timeout: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStatusReportTravelsOnSecureChannel: a StatusReport sent on an IM
// exchange carries protocol 0 (MessageExchange.ts:662-665).
func TestStatusReportTravelsOnSecureChannel(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, _ := newUnsecured(t, m)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	if err := x.Send(context.Background(), mrp.SCOpcodeStatusReport, nil, SendOptions{SuppressAck: true}); err != nil {
		t.Fatal(err)
	}
	d := decodeUnsecured(t, conn.next(t).bytes)
	if d.proto.ProtocolID != mrp.SecureChannelProtocolID || d.proto.NeedsAck {
		t.Fatalf("status report header %+v", d.proto)
	}
	if err := x.Send(context.Background(), im.OpcodeStatusResponse, nil, SendOptions{DisableMRP: true}); err != nil {
		t.Fatal(err)
	}
	d = decodeUnsecured(t, conn.next(t).bytes)
	if d.proto.ProtocolID != im.InteractionModelProtocolID || !d.proto.NeedsAck || x.HasUnackedMessage() {
		t.Fatalf("DisableMRP send %+v (unacked %v)", d.proto, x.HasUnackedMessage())
	}
}

// TestRetransmissionSchedule: an unacknowledged message is sent
// MaxTransmissions times with the backoff of MRP.ts:162-190 for an idle
// peer (the unsecured session of an initiator starts idle), then the
// final wait of MessageExchange.ts:894-918, then PeerUnresponsiveError.
func TestRetransmissionSchedule(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, _ := newUnsecured(t, m)
	if s.IsPeerActive() {
		t.Fatal("the initiator's unsecured session starts with the peer active")
	}
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, []byte{1}, SendOptions{})
	first := conn.nextOn(t, x)
	// Idle 500 ms, jitter 0.5: 500·1.1·1.125 = 618.75 → 618, ·1.6 → 990,
	// ·2.56 → 1584, ·4.096 → 2534.4 → 2534.
	backoffs := []time.Duration{618, 618, 990, 1584, 2534}
	for i, b := range backoffs[:4] {
		clock.Advance(b * time.Millisecond)
		again := conn.next(t)
		if !bytes.Equal(again.bytes, first.bytes) {
			t.Fatalf("retransmission %d differs from the first transmission", i+1)
		}
	}
	clock.Advance(backoffs[4] * time.Millisecond)
	conn.none(t)
	// Final wait: MaxPeerResponseTime(local, 2 s) = 11229 ms less the last backoff.
	final := 11229*time.Millisecond - backoffs[4]*time.Millisecond
	got := clock.durations()
	want := []time.Duration{618 * time.Millisecond, 618 * time.Millisecond, 990 * time.Millisecond, 1584 * time.Millisecond, 2534 * time.Millisecond, final}
	if len(got) != len(want) {
		t.Fatalf("timers %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("timer %d = %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
	clock.Advance(final - time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Send returned %v before the final wait ended", err)
	default:
	}
	clock.Advance(time.Millisecond)
	err := waitErr(t, done)
	var unresponsive *PeerUnresponsiveError
	if !errors.As(err, &unresponsive) || !errors.Is(err, ErrTransient) || unresponsive.Waited != 15039*time.Millisecond {
		t.Fatalf("Send = %v, want PeerUnresponsiveError after 15039 ms", err)
	}
	if conn.count() != MaxTransmissions || !x.HasUnackedMessage() {
		t.Fatalf("%d transmissions (unacked %v), want %d", conn.count(), x.HasUnackedMessage(), MaxTransmissions)
	}
}

// TestRetransmissionOptions: MaxRetransmissions bounds the transmissions,
// MaxRetransmissionTime caps the backoff no lower than the idle interval
// and InitialRetransmissionTime is added (MessageExchange.ts:1203-1220);
// an active peer is retried on its active interval.
func TestRetransmissionOptions(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)
	p.reply(0x9000, 0, 0x40, false, nil, nil) // the peer is now active
	if !s.IsPeerActive() {
		t.Fatal("peer not active after a message")
	}
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, nil, SendOptions{MaxRetransmissions: 2, MaxRetransmissionTime: time.Millisecond, InitialRetransmissionTime: 200 * time.Millisecond, ExpectedProcessingTime: time.Millisecond})
	conn.nextOn(t, x)
	// Active 300 ms: 300·1.1·1.125 = 371.25 → 371, +200 = 571, capped at
	// max(1 ms, idle 500 ms) = 500 ms.
	if d := clock.durations(); len(d) != 1 || d[0] != 500*time.Millisecond {
		t.Fatalf("first backoff %v, want 500ms", d)
	}
	clock.Advance(500 * time.Millisecond)
	conn.next(t)
	// Two transmissions spent; the final wait — 4229 ms + 1 ms + 5 s less
	// the last backoff — follows, then the failure.
	clock.Advance(500 * time.Millisecond)
	if d := clock.durations(); d[len(d)-1] != 8730*time.Millisecond {
		t.Fatalf("final wait %v, want 8730ms", d[len(d)-1])
	}
	clock.Advance(time.Hour)
	if err := waitErr(t, done); !errors.Is(err, ErrTransient) {
		t.Fatalf("Send = %v", err)
	}
	if conn.count() != 2 {
		t.Fatalf("%d transmissions, want 2", conn.count())
	}
}

// TestSendContextCancelled: a ctx already done sends nothing; one
// cancelled while waiting stops the retransmissions and leaves the
// message unacknowledged (MessageExchange.ts:793-805).
func TestSendContextCancelled(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, _ := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := x.Send(ctx, 0x20, nil, SendOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send(done ctx) = %v", err)
	}
	conn.none(t)
	ctx, cancel = context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- x.Send(ctx, 0x20, nil, SendOptions{}) }()
	conn.next(t)
	cancel()
	if err := waitErr(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send = %v", err)
	}
	clock.Advance(time.Hour)
	conn.none(t)
	if !x.HasUnackedMessage() {
		t.Fatal("the cancelled message counts as acknowledged")
	}
}

// TestNextTimeoutIsMaxPeerResponseTime: Next waits MaxPeerResponseTime of
// our own intervals plus the processing time (MessageExchange.ts:834-860)
// and then fails with PeerMessageMissingError.
func TestNextTimeoutIsMaxPeerResponseTime(t *testing.T) {
	t.Parallel()
	m, _, clock := newTestManager(t, Config{})
	s, _ := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	for _, tc := range []struct {
		ept  time.Duration
		want time.Duration
	}{
		{0, 11229 * time.Millisecond},
		{35 * time.Second, 44229 * time.Millisecond},
	} {
		before := len(clock.durations())
		done := make(chan error, 1)
		go func() {
			_, err := x.Next(context.Background(), ReceiveOptions{ExpectedProcessingTime: tc.ept})
			done <- err
		}()
		deadline := time.Now().Add(5 * time.Second)
		for len(clock.durations()) == before && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if d := clock.durations(); d[len(d)-1] != tc.want {
			t.Fatalf("Next timeout %v, want %v", d[len(d)-1], tc.want)
		}
		clock.Advance(tc.want)
		err := waitErr(t, done)
		var missing *PeerMessageMissingError
		if !errors.As(err, &missing) || missing.Timeout != tc.want || !errors.Is(err, ErrTransient) || !errors.Is(err, &PeerUnresponsiveError{}) {
			t.Fatalf("Next = %v", err)
		}
	}
}

// TestCloseBehaviour: an unused exchange closes at once; Close acks an
// owed message at once; with a message of ours unacknowledged it waits
// for the remaining transmissions and closes on a late standalone ack
// (MessageExchange.ts:1053-1130, :513-516); a closed exchange refuses.
func TestCloseBehaviour(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newUnsecured(t, m)

	unused, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	unused.Close()
	<-unused.Done()
	if err := unused.Send(context.Background(), 0x20, nil, SendOptions{}); !errors.Is(err, ErrExchangeClosed) {
		t.Fatalf("Send on closed = %v", err)
	}
	if _, err := unused.Next(context.Background(), ReceiveOptions{}); !errors.Is(err, ErrExchangeClosed) {
		t.Fatalf("Next on closed = %v", err)
	}

	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, nil, SendOptions{})
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	c := p.reply(x.ID(), 0, 0x40, true, u32(first.hdr.MessageCounter), nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	x.Close()
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.AckCounter != c {
		t.Fatalf("Close did not ack: %+v", ack.proto)
	}
	<-x.Done()

	y, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done = sendAsync(y, 0x20, nil, SendOptions{})
	pending := decodeUnsecured(t, conn.nextOn(t, y).bytes)
	y.Close()
	select {
	case <-y.Done():
		t.Fatal("closed with a message unacknowledged")
	default:
	}
	// The close timer is the maximum of the remaining transmissions; a
	// message after Close is acked and closes the exchange, a late
	// standalone ack closes it.
	p.reply(y.ID(), 0, mrp.StandaloneAckOpcode, false, u32(pending.hdr.MessageCounter), nil)
	<-y.Done()
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}

	z, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done = sendAsync(z, 0x20, nil, SendOptions{})
	conn.nextOn(t, z)
	z.Close()
	// Data arriving on a closing exchange is acked and the exchange closed.
	c = p.reply(z.ID(), 0, 0x21, true, nil, nil)
	if a := decodeUnsecured(t, conn.next(t).bytes); a.proto.AckCounter != c {
		t.Fatalf("ack on closing exchange %+v", a.proto)
	}
	clock.Advance(time.Hour)
	<-z.Done()
	_ = waitErr(t, done)

	w, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done = sendAsync(w, 0x20, nil, SendOptions{})
	conn.nextOn(t, w)
	w.Abort(errors.New("shut down"))
	<-w.Done()
	if err := waitErr(t, done); err != nil {
		t.Fatalf("Send resolved by Abort = %v (matter.js resolves it, MessageExchange.ts:1145)", err)
	}
	if err := w.Send(context.Background(), 0x20, nil, SendOptions{}); err == nil || err.Error() != "exchange: exchange closed: shut down" {
		t.Fatalf("Send after Abort = %v", err)
	}
	if clock.pendingTimers() != 0 {
		t.Fatalf("%d timers left running", clock.pendingTimers())
	}
}

// TestCloseTimerExpires: the close timer ends an exchange whose message
// is never acknowledged.
func TestCloseTimerExpires(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, _ := newUnsecured(t, m)
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	done := sendAsync(x, 0x20, nil, SendOptions{})
	conn.nextOn(t, x)
	x.Close()
	x.Close()
	// Remaining transmissions 0..5 at their maximum for the idle peer:
	// 687 + 687 + 1100 + 1760 + 2816 + 4505 = 11555 ms.
	d := clock.durations()
	if d[len(d)-1] != 11555*time.Millisecond {
		t.Fatalf("close timer %v, want 11555ms", d[len(d)-1])
	}
	clock.Advance(618 * time.Millisecond) // the retransmission timer sees the exchange closing
	<-x.Done()
	if err := waitErr(t, done); !errors.Is(err, ErrTransient) {
		t.Fatalf("Send = %v", err)
	}
}

// TestExchangeCapEvictsLeastRecentlyActive: a secure session keeps 30
// open exchanges; the 31st closes the least recently active one
// (ExchangeManager.ts:503-531). The unsecured session is exempt.
func TestExchangeCapEvictsLeastRecentlyActive(t *testing.T) {
	t.Parallel()
	m, _, clock := newTestManager(t, Config{})
	s, _ := newSecurePair(t, m, 10, false)
	xs := make([]*Exchange, 0, MaxConcurrentExchangesPerSession)
	for range MaxConcurrentExchangesPerSession {
		x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{})
		if err != nil {
			t.Fatal(err)
		}
		xs = append(xs, x)
		clock.Advance(time.Millisecond)
	}
	for _, x := range xs {
		select {
		case <-x.Done():
			t.Fatal("an exchange closed below the cap")
		default:
		}
	}
	if _, err := m.Initiate(s, im.InteractionModelProtocolID, Options{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-xs[0].Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the least recently active exchange was not closed")
	}
	u, _ := newUnsecured(t, m)
	ux := make([]*Exchange, 0, MaxConcurrentExchangesPerSession+1)
	for range MaxConcurrentExchangesPerSession + 1 {
		x, _ := m.Initiate(u, mrp.SecureChannelProtocolID, Options{})
		ux = append(ux, x)
	}
	for _, x := range ux {
		select {
		case <-x.Done():
			t.Fatal("an unsecured exchange was evicted")
		default:
		}
	}
}

// TestSecureSessionRoundTrip: a secure exchange encrypts under the peer's
// session id, decrypts the reply, and a duplicate is acked once.
func TestSecureSessionRoundTrip(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0101, false)
	if !s.IsSecure() || s.ID() != 0x0101 || s.PeerSessionID() != 0x7101 || s.NodeID() != 0x1111 || s.PeerNodeID() != 0x2222 ||
		s.Addr() != peerAddr || !s.IsPeerActive() || s.Resumed() || s.ReportedParameters() != nil {
		t.Fatalf("session accessors: %+v", s)
	}
	if m.Session(0x0101) != s {
		t.Fatal("Session() lookup")
	}
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	done := sendAsync(x, im.OpcodeReadRequest, []byte{7}, SendOptions{})
	out := p.decode(t, conn.nextOn(t, x).bytes)
	if out.hdr.SessionID != 0x7101 || out.proto.Opcode != im.OpcodeReadRequest || string(out.payload) != "\x07" {
		t.Fatalf("secure send %+v %+v", out.hdr, out.proto)
	}
	reply := message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: out.hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}
	raw := p.send(t, 0x0101, reply, []byte{8})
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	m.HandleDatagram(raw, peerAddr) // the duplicate
	ack := p.decode(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode {
		t.Fatalf("duplicate ack %+v", ack.proto)
	}
	if msg, err := x.Next(context.Background(), ReceiveOptions{}); err != nil || string(msg.Payload) != "\x08" {
		t.Fatalf("Next = %+v, %v", msg, err)
	}
	// A tampered datagram is dropped.
	raw[len(raw)-1] ^= 0xFF
	m.HandleDatagram(raw, peerAddr)
	conn.none(t)
}

// TestPeerLostHook: a send that fails on an exchange that never heard
// from the peer reports it on a secure session; a suppressed session, an
// exchange that received, and the unsecured session do not.
func TestPeerLostHook(t *testing.T) {
	t.Parallel()
	lost := make(chan *Session, 4)
	m, conn, clock := newTestManager(t, Config{PeerLost: func(s *Session, _ error) { lost <- s }})
	s, _ := newSecurePair(t, m, 0x0202, false)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	conn.nextOn(t, x)
	clock.Advance(time.Hour)
	clock.Advance(time.Hour)
	if err := waitErr(t, done); !errors.Is(err, ErrTransient) {
		t.Fatal(err)
	}
	select {
	case got := <-lost:
		if got != s {
			t.Fatal("PeerLost names another session")
		}
	default:
		t.Fatal("PeerLost not called")
	}
	quiet, _ := newSecurePair(t, m, 0x0203, true)
	y, _ := m.Initiate(quiet, im.InteractionModelProtocolID, Options{})
	before := len(clock.durations())
	next := make(chan error, 1)
	go func() {
		_, err := y.Next(context.Background(), ReceiveOptions{Timeout: time.Second})
		next <- err
	}()
	for len(clock.durations()) == before {
		time.Sleep(time.Millisecond)
	}
	clock.Advance(time.Second)
	if err := waitErr(t, next); !errors.Is(err, ErrTransient) {
		t.Fatalf("Next = %v, want a timeout", err)
	}
	u, _ := newUnsecured(t, m)
	z, _ := m.Initiate(u, 0, Options{})
	conn.fail = errors.New("boom")
	if err := z.Send(context.Background(), 0x20, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
		t.Fatalf("Send on failing socket = %v", err)
	}
	select {
	case <-lost:
		t.Fatal("PeerLost called for a suppressed or unsecured session")
	default:
	}
}

// TestCloseSessionSendsCloseSession: a secure session is closed with an
// unacknowledged StatusReport (Success, CloseSession) on a fresh exchange
// (ExchangeManager.ts:716-726, SecureChannelMessenger.ts:193-195), its
// exchanges end with ErrSessionClosed and it leaves the table.
func TestCloseSessionSendsCloseSession(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0303, false)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	m.CloseSession(context.Background(), s)
	out := p.decode(t, conn.next(t).bytes)
	r, ok := DecodeStatusReport(out.payload)
	if !ok || out.proto.Opcode != mrp.SCOpcodeStatusReport || out.proto.NeedsAck || r.GeneralCode != 0 || r.ProtocolCode != 3 || r.ProtocolID != 0 {
		t.Fatalf("CloseSession %+v %+v", out.proto, r)
	}
	if _, err := x.Next(context.Background(), ReceiveOptions{}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Next after CloseSession = %v", err)
	}
	if !s.IsClosed() || m.Session(0x0303) != nil {
		t.Fatal("session still registered")
	}
	if _, err := m.Initiate(s, 0, Options{}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Initiate on closed = %v", err)
	}
	m.CloseSession(context.Background(), s) // idempotent
	conn.none(t)
}

// TestSessionIDAllocation: ids cycle from a cursor, skip ids in use, and
// when all are taken the least recently used session is closed and its
// id reused (SessionManager.ts:504-557).
func TestSessionIDAllocation(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	m.sessionIDLimit = 3
	m.nextSessionID = 2
	ctx := context.Background()
	got := make([]uint16, 0, 3)
	for range 3 {
		id, err := m.AllocateSessionID(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
		newSecurePair(t, m, id, false)
		clock.Advance(time.Second)
	}
	if got[0] != 2 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("ids %v, want [2 3 1]", got)
	}
	// Every id is taken; session 2 has the oldest traffic.
	id, err := m.AllocateSessionID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != 2 || m.Session(2) != nil {
		t.Fatalf("reused id %d (session 2 still open: %v), want 2", id, m.Session(2) != nil)
	}
	if conn.count() != 1 {
		t.Fatalf("%d datagrams, want the CloseSession of the evicted session", conn.count())
	}
	if _, err := m.NewSecureSession(SecureSessionConfig{ID: 1, Addr: peerAddr}); err == nil {
		t.Fatal("a taken id was registered twice")
	}
	if _, err := m.NewSecureSession(SecureSessionConfig{ID: 0, Addr: peerAddr}); err == nil {
		t.Fatal("session id 0 accepted")
	}
	if _, err := m.NewSecureSession(SecureSessionConfig{ID: 9}); err == nil {
		t.Fatal("nil address accepted")
	}
}

// TestUnsecuredSessionParameters: SetTimingParameters replaces the
// intervals with the fallback for each one not stated
// (Session.ts:249-254); the session's parameters start from what the
// discovery knew.
func TestUnsecuredSessionParameters(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	s, err := m.NewUnsecuredSession(peerAddr, &sessionparams.Parameters{SessionIdleInterval: 900, DataModelRevision: 19})
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Parameters(); p.SessionIdleInterval != 900 || p.SessionActiveInterval != 300 || p.DataModelRevision != 19 {
		t.Fatalf("parameters %+v", p)
	}
	s.SetTimingParameters(sessionparams.Parameters{SessionActiveInterval: 50})
	if p := s.Parameters(); p.SessionIdleInterval != 500 || p.SessionActiveInterval != 50 || p.SessionActiveThreshold != 4000 || p.DataModelRevision != 19 {
		t.Fatalf("after SetTimingParameters %+v", p)
	}
	if s.IsSecure() || s.PeerNodeID() != 0 || s.NodeID() == 0 || s.CaseAuthenticatedTags() != nil || len(s.AttestationChallenge()) != 0 {
		t.Fatal("unsecured session accessors")
	}
	if _, err := m.NewUnsecuredSession(nil, nil); err == nil {
		t.Fatal("nil address accepted")
	}
	m.Close()
	if !s.IsClosed() {
		t.Fatal("Manager.Close left the session open")
	}
	if _, err := m.NewUnsecuredSession(peerAddr, nil); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("NewUnsecuredSession after Close = %v", err)
	}
	if _, err := m.NewSecureSession(SecureSessionConfig{ID: 5, Addr: peerAddr, Channel: newSecureConfig()}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("NewSecureSession after Close = %v", err)
	}
	if _, err := NewManager(nil, Config{}); err == nil {
		t.Fatal("nil Conn accepted")
	}
}

// TestDefaultsAndAccessors: the system clock and math/rand defaults, the
// local parameters, and the reported parameters a secure session keeps.
func TestDefaultsAndAccessors(t *testing.T) {
	t.Parallel()
	m, err := NewManager(newFakeConn(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.LocalParameters() != DefaultLocalParameters() {
		t.Fatalf("LocalParameters = %+v", m.LocalParameters())
	}
	fired := make(chan struct{})
	if (SystemClock{}).Now().IsZero() {
		t.Fatal("SystemClock.Now is zero")
	}
	SystemClock{}.AfterFunc(time.Millisecond, func() { close(fired) })
	<-fired
	own := sessionparams.Parameters{SessionIdleInterval: 1000}
	custom, err := NewManager(newFakeConn(), Config{LocalParameters: &own})
	if err != nil {
		t.Fatal(err)
	}
	defer custom.Close()
	if custom.LocalParameters() != own {
		t.Fatalf("LocalParameters = %+v", custom.LocalParameters())
	}
	reported := sessionparams.Parameters{MaxPathsPerInvoke: 4}
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID: 7, Addr: peerAddr, Channel: newSecureConfig(), PeerParameters: PeerParameters(&reported),
		ReportedParameters: &reported, CaseAuthenticatedTags: []uint32{0x0001_0001}, AttestationChallenge: []byte{1, 2}, Resumed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.ReportedParameters(); r == nil || *r != reported || s.Parameters().MaxPathsPerInvoke != 4 ||
		len(s.CaseAuthenticatedTags()) != 1 || len(s.AttestationChallenge()) != 2 || !s.Resumed() {
		t.Fatalf("secure session accessors: %+v", s)
	}
	if _, err := m.NewSecureSession(SecureSessionConfig{ID: 8, Addr: peerAddr}); err == nil {
		t.Fatal("a session without keys was accepted")
	}
}
