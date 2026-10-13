// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// handled is one call of a test ProtocolHandler.
type handled struct {
	x     *Exchange
	first *Message
}

// recordingHandler returns a handler that reports each call on the
// channel and then waits for release.
func recordingHandler(release <-chan struct{}) (h ProtocolHandler, calls <-chan handled) {
	ch := make(chan handled, 8)
	return func(x *Exchange, first *Message) {
		ch <- handled{x: x, first: first}
		if release != nil {
			<-release
		}
	}, ch
}

func waitHandled(t *testing.T, ch <-chan handled) handled {
	t.Helper()
	select {
	case h := <-ch:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called")
		return handled{}
	}
}

// TestHandlerTakesPeerOpenedIMExchange: a handler for the IM protocol
// receives the first message of an exchange the peer opens on a secure
// session (ExchangeManager.ts:407-440); the message is not queued again for
// Next; the receive path does not wait for the handler — a second exchange
// reaches it while the first is still running; when the handler returns
// the exchange closes and the owed ack goes out at once.
func TestHandlerTakesPeerOpenedIMExchange(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0401, false)
	release := make(chan struct{})
	h, calls := recordingHandler(release)
	m.SetProtocolHandler(im.InteractionModelProtocolID, h)

	p.send(t, s.ID(), peerOpens(0x6001, im.InteractionModelProtocolID, im.OpcodeReportData, true), []byte{0x15, 0x18})
	first := waitHandled(t, calls)
	if first.first.Opcode != im.OpcodeReportData || first.x.Session() != s || first.x.ID() != 0x6001 {
		t.Fatalf("handler got %+v on exchange %d", first.first, first.x.ID())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if msg, err := first.x.Next(ctx, ReceiveOptions{Timeout: time.Hour}); err == nil {
		t.Fatalf("Next returned the first message again: %+v", msg)
	}
	cancel()
	// The receive path is free: a second exchange is dispatched while the
	// first handler blocks.
	p.send(t, s.ID(), peerOpens(0x6002, im.InteractionModelProtocolID, im.OpcodeReportData, true), []byte{0x15, 0x18})
	second := waitHandled(t, calls)
	if second.x.ID() != 0x6002 {
		t.Fatalf("second handler on exchange %d", second.x.ID())
	}
	close(release)
	for range 2 {
		ack := p.decode(t, conn.next(t).bytes)
		if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.Initiator || !ack.proto.HasAck {
			t.Fatalf("ack %+v", ack.proto)
		}
	}
	for _, x := range []*Exchange{first.x, second.x} {
		select {
		case <-x.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("exchange not closed after its handler returned")
		}
	}
	// Removing the handler: the next message is acknowledged and dropped.
	m.SetProtocolHandler(im.InteractionModelProtocolID, nil)
	p.send(t, s.ID(), peerOpens(0x6003, im.InteractionModelProtocolID, im.OpcodeReportData, true), nil)
	if ack := p.decode(t, conn.next(t).bytes); ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.ExchangeID != 0x6003 {
		t.Fatalf("ack %+v", ack.proto)
	}
	select {
	case h := <-calls:
		t.Fatalf("removed handler called for exchange %d", h.x.ID())
	default:
	}
}

// TestHandlerNotForUnsecuredOrSecureChannel: an IM handler is not
// consulted on an unsecured session; a Secure Channel handler is not
// consulted on a secure session (the built-in one stays).
func TestHandlerNotForUnsecuredOrSecureChannel(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0402, false)
	h, calls := recordingHandler(nil)
	m.SetProtocolHandler(im.InteractionModelProtocolID, h)
	m.SetProtocolHandler(mrp.SecureChannelProtocolID, h)
	p.send(t, s.ID(), peerOpens(0x6004, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, true), nil)
	if ack := p.decode(t, conn.next(t).bytes); ack.proto.Opcode != mrp.StandaloneAckOpcode {
		t.Fatalf("ack %+v", ack.proto)
	}
	unsecuredRequest(m, 0x7777, 1, 0x6005, im.InteractionModelProtocolID, im.OpcodeReportData, nil)
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.hdr.DestNodeID != 0x7777 {
		t.Fatalf("unsecured ack %+v / %+v", ack.hdr, ack.proto)
	}
	select {
	case c := <-calls:
		t.Fatalf("handler called for %+v", c.first)
	case <-time.After(20 * time.Millisecond):
	}
	if n := countInbound(m); n != 0 {
		t.Fatalf("%d inbound sessions left", n)
	}
}

// unsecuredRequest delivers a peer-initiated unsecured message from source
// node id src.
func unsecuredRequest(m *Manager, src uint64, counter uint32, xid, protocol uint16, opcode uint8, payload []byte) {
	hdr := message.Header{MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: src}
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: protocol}
	m.HandleDatagram(append(append(hdr.Marshal(), proto.Marshal()...), payload...), peerAddr)
}

func countInbound(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.unsecured {
		if s.inbound {
			n++
		}
	}
	return n
}

// TestSigma1ReachesHandlerOnInboundSession: a Sigma1 on an unsecured
// session the peer opens reaches the Secure Channel handler
// (ExchangeManager.ts:261-264, SecureChannelProtocol.ts:146-148): the
// session is keyed by the peer's source node id, our messages name it as
// destination, a retransmitted Sigma1 is acknowledged on the same exchange,
// the peer's next message reaches the handler's Next, and the session is
// closed once the handler is done.
func TestSigma1ReachesHandlerOnInboundSession(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	got := make(chan handled, 1)
	done := make(chan struct{})
	m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, first *Message) {
		got <- handled{x: x, first: first}
		<-done
	})
	unsecuredRequest(m, 0x8888, 10, 0x6100, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
	h := waitHandled(t, got)
	s := h.x.Session()
	if s.IsSecure() || !s.IsInbound() || s.NodeID() != 0 || h.first.Opcode != mrp.SCOpcodeSigma1 {
		t.Fatalf("session secure %v inbound %v node %X, first %+v", s.IsSecure(), s.IsInbound(), s.NodeID(), h.first)
	}
	// A retransmission of Sigma1 is a duplicate: acknowledged on the
	// exchange, not a second handler call.
	unsecuredRequest(m, 0x8888, 10, 0x6100, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
	dup := decodeUnsecured(t, conn.next(t).bytes)
	if dup.proto.Opcode != mrp.StandaloneAckOpcode || dup.hdr.DestSize != message.DestNodeID || dup.hdr.DestNodeID != 0x8888 || dup.hdr.HasSourceNodeID {
		t.Fatalf("duplicate ack %+v / %+v", dup.hdr, dup.proto)
	}
	// The handler answers; the reply names the peer as destination and
	// carries the owed ack.
	sendDone := sendAsync(h.x, mrp.SCOpcodeSigma2, []byte{1}, SendOptions{})
	reply := decodeUnsecured(t, conn.nextOn(t, h.x).bytes)
	if reply.proto.Opcode != mrp.SCOpcodeSigma2 || reply.proto.Initiator || reply.hdr.DestNodeID != 0x8888 || !reply.proto.HasAck || reply.proto.AckCounter != 10 {
		t.Fatalf("reply %+v / %+v", reply.hdr, reply.proto)
	}
	// The peer's Sigma3 acknowledges it and reaches Next.
	hdr := message.Header{MessageCounter: 11, HasSourceNodeID: true, SourceNodeID: 0x8888}
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma3, ExchangeID: 0x6100, ProtocolID: mrp.SecureChannelProtocolID, HasAck: true, AckCounter: reply.hdr.MessageCounter}
	m.HandleDatagram(append(hdr.Marshal(), proto.Marshal()...), peerAddr)
	if err := waitErr(t, sendDone); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg, err := h.x.Next(context.Background(), ReceiveOptions{Timeout: time.Second})
	if err != nil || msg.Opcode != mrp.SCOpcodeSigma3 {
		t.Fatalf("Next = %+v, %v", msg, err)
	}
	close(done)
	select {
	case <-s.Closing():
	case <-time.After(5 * time.Second):
		t.Fatal("inbound session not closed after the handler")
	}
	if n := countInbound(m); n != 0 {
		t.Fatalf("%d inbound sessions left", n)
	}
}

// TestSigma1WithoutHandlerIsAcknowledged: without a Secure Channel handler
// a Sigma1 is only acknowledged; an unsecured request without a source
// node id is dropped; no inbound session remains.
func TestSigma1WithoutHandlerIsAcknowledged(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	unsecuredRequest(m, 0x9999, 1, 0x6200, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, nil)
	ack := decodeUnsecured(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.AckCounter != 1 || ack.hdr.DestNodeID != 0x9999 {
		t.Fatalf("ack %+v / %+v", ack.hdr, ack.proto)
	}
	hdr := message.Header{MessageCounter: 2}
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma1, ExchangeID: 0x6201}
	m.HandleDatagram(append(hdr.Marshal(), proto.Marshal()...), peerAddr)
	conn.none(t)
	if n := countInbound(m); n != 0 {
		t.Fatalf("%d inbound sessions left", n)
	}
}

// TestPBKDFParamRequestIsRefused: a PBKDFParamRequest is answered with a
// StatusReport Failure / InvalidParam (SecureChannelProtocol.ts:129-137);
// once acknowledged the exchange and the inbound session close.
func TestPBKDFParamRequestIsRefused(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	unsecuredRequest(m, 0xAAAA, 5, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodePBKDFParamRequest, []byte{0x15, 0x18})
	d := decodeUnsecured(t, conn.next(t).bytes)
	if d.proto.Opcode != mrp.SCOpcodeStatusReport || d.hdr.DestNodeID != 0xAAAA || !d.proto.HasAck || d.proto.AckCounter != 5 || !d.proto.NeedsAck {
		t.Fatalf("refusal %+v / %+v", d.hdr, d.proto)
	}
	r, ok := DecodeStatusReport(d.payload)
	if !ok || r.GeneralCode != mrp.SCStatusGeneralFailure || r.ProtocolCode != mrp.SCStatusProtocolInvalidParameter {
		t.Fatalf("status %+v", r)
	}
	hdr := message.Header{MessageCounter: 6, HasSourceNodeID: true, SourceNodeID: 0xAAAA}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.StandaloneAckOpcode, ExchangeID: 0x6300, ProtocolID: mrp.SecureChannelProtocolID, HasAck: true, AckCounter: d.hdr.MessageCounter}
	m.HandleDatagram(append(hdr.Marshal(), proto.Marshal()...), peerAddr)
	deadline := time.Now().Add(5 * time.Second)
	for countInbound(m) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("inbound session left after the refusal")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPeerOpenedExchangeOnClosingSessionDeclined: a session that is lost
// declines a new exchange (ExchangeManager.ts:386-389) — no handler call,
// no ack.
func TestPeerOpenedExchangeOnClosingSessionDeclined(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0403, false)
	h, calls := recordingHandler(nil)
	m.SetProtocolHandler(im.InteractionModelProtocolID, h)
	b := func() []byte {
		hdr := message.Header{SessionID: s.ID()}
		sealed, err := p.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(peerOpens(0x6400, im.InteractionModelProtocolID, im.OpcodeReportData, true).Marshal(), 0x15, 0x18))
		if err != nil {
			t.Fatal(err)
		}
		return append(hdr.Marshal(), sealed.Ciphertext...)
	}()
	s.mu.Lock()
	s.peerLost = true
	s.mu.Unlock()
	m.HandleDatagram(b, peerAddr)
	conn.none(t)
	select {
	case c := <-calls:
		t.Fatalf("handler called on a lost session: %+v", c.first)
	default:
	}
}
