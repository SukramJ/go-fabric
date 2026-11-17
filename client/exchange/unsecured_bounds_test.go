// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestUnauthenticatedTimingIsClamped: the intervals a peer states before
// it has authenticated — in its DNS-SD record or in a handshake message —
// time the unsecured session at most one hour each (review II, owner
// decision 45, R2-P2-alloc-02).
func TestUnauthenticatedTimingIsClamped(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	huge := sessionparams.Parameters{SessionIdleInterval: 0xFFFFFFFF, SessionActiveInterval: 0xFFFFFFFF, SessionActiveThreshold: 0xFFFF}
	s, err := m.NewUnsecuredSession(peerAddr, &huge)
	if err != nil {
		t.Fatal(err)
	}
	check := func(stage string) {
		t.Helper()
		p := s.Parameters()
		if p.SessionIdleInterval != sessionparams.MaxUnauthenticatedInterval || p.SessionActiveInterval != sessionparams.MaxUnauthenticatedInterval || p.SessionActiveThreshold != 0xFFFF {
			t.Fatalf("%s: intervals %d / %d / %d", stage, p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold)
		}
	}
	check("from the DNS-SD record")
	s.SetTimingParameters(sessionparams.Parameters{SessionIdleInterval: 500})
	s.SetTimingParameters(huge)
	check("from the handshake")
}

// TestInboundUnsecuredAckWaitsAreBounded: at most 16 messages on the
// sessions peers open without authenticating wait for an acknowledgement;
// the next one ends the oldest wait, whose exchange and session then close
// (review II, R2-P2-alloc-02).
func TestInboundUnsecuredAckWaitsAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		for src := uint64(1); src <= inboundUnsecuredAckWaitLimit+1; src++ {
			unsecuredRequest(m, src, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodePBKDFParamRequest, []byte{0x15, 0x18})
			synctest.Wait()
		}
		if n := countInbound(m); n != inboundUnsecuredAckWaitLimit {
			t.Fatalf("%d inbound sessions wait, want %d", n, inboundUnsecuredAckWaitLimit)
		}
		m.mu.Lock()
		waits, oldest := len(m.ackWaits), m.unsecured[1]
		m.mu.Unlock()
		if waits != inboundUnsecuredAckWaitLimit || oldest != nil {
			t.Fatalf("%d waits held, the oldest kept: %v", waits, oldest != nil)
		}
	})
}

// TestInboundUnsecuredSessionLifetime: a session a peer opened without
// authenticating closes 60 s after it was opened, whatever its handshake
// waits for (review II, R2-P2-alloc-02).
func TestInboundUnsecuredSessionLifetime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, clock := newTestManager(t, Config{})
		release := make(chan struct{})
		defer close(release)
		h, calls := recordingHandler(release)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, h)
		unsecuredRequest(m, 0xAAAA, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		c := waitHandled(t, calls)
		synctest.Wait()
		clock.Advance(inboundUnsecuredLifetime - time.Millisecond)
		synctest.Wait()
		if c.x.Session().IsClosed() || countInbound(m) != 1 {
			t.Fatal("closed before its lifetime")
		}
		clock.Advance(time.Millisecond)
		synctest.Wait()
		if !c.x.Session().IsClosed() || countInbound(m) != 0 {
			t.Fatal("open after its lifetime")
		}
	})
}

// TestInboundUnsecuredSessionsAreBounded: at most 256 sessions peers
// opened without authenticating are held; the next one closes the oldest
// (review II, R2-P2-alloc-02).
func TestInboundUnsecuredSessionsAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		release := make(chan struct{})
		defer close(release)
		calls := make(chan *Exchange, maxInboundUnsecuredSessions+1)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, _ *Message) {
			calls <- x
			<-release
		})
		for src := uint64(1); src <= maxInboundUnsecuredSessions+1; src++ {
			unsecuredRequest(m, src, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		}
		synctest.Wait()
		if n := countInbound(m); n != maxInboundUnsecuredSessions {
			t.Fatalf("%d inbound sessions, want %d", n, maxInboundUnsecuredSessions)
		}
		m.mu.Lock()
		first := m.unsecured[1]
		m.mu.Unlock()
		closed := 0
		for range maxInboundUnsecuredSessions + 1 {
			x := <-calls
			if x.Session().IsClosed() {
				closed++
				if x.Session().nodeID != 1 {
					t.Fatalf("closed the session of %#x, not the oldest", x.Session().nodeID)
				}
			}
		}
		if first != nil || closed != 1 {
			t.Fatalf("the oldest session was kept (closed %d)", closed)
		}
	})
}

// TestInboundUnsecuredSessionHoldsOneHandshake: a session a peer opened
// without authenticating holds one exchange a Sigma1 opened, as one
// handshake needs; a further Sigma1 starts no handler while it is open, so
// one source cannot hold handlers beyond the session bound (review II,
// R2-P2-dr-ctl-04) — it is answered Busy ([Manager.answerBusy]; review II,
// R2-P6-par-2902). Once the handshake ended and its session closed, the
// source's next Sigma1 opens a session and reaches the handler again.
// Before, every Sigma1 with a fresh exchange id started a handler on the one
// session.
func TestInboundUnsecuredSessionHoldsOneHandshake(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		release := make(chan struct{})
		end := sync.OnceFunc(func() { close(release) })
		defer end()
		h, calls := recordingHandler(release)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, h)
		const sigma1s = 8
		for i := range uint16(sigma1s) {
			unsecuredRequest(m, 0xAAAA, uint32(i)+1, 0x6300+i, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		}
		synctest.Wait()
		first := waitHandled(t, calls)
		if n := len(calls); n != 0 {
			t.Fatalf("%d further handlers run on the session, want none", n)
		}
		s := first.x.Session()
		s.mu.Lock()
		places := s.handshakes
		s.mu.Unlock()
		if places != 1 || countInbound(m) != 1 {
			t.Fatalf("%d handshake places on %d inbound sessions, want 1 on 1", places, countInbound(m))
		}
		// Every refused Sigma1 is answered Busy on its exchange, which
		// acknowledges it; the peer acknowledges the answers.
		answered := map[uint16]bool{}
		for range sigma1s - 1 {
			busy := decodeUnsecured(t, conn.next(t).bytes)
			expectBusy(t, busy, 5000)
			answered[busy.proto.ExchangeID] = true
			ackCounter := busy.hdr.MessageCounter
			ack := message.Header{MessageCounter: uint32(sigma1s) + uint32(busy.proto.ExchangeID), HasSourceNodeID: true, SourceNodeID: 0xAAAA}
			proto := message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: ackCounter, Opcode: mrp.StandaloneAckOpcode, ExchangeID: busy.proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID}
			m.HandleDatagram(append(ack.Marshal(), proto.Marshal()...), peerAddr)
		}
		for i := range uint16(sigma1s - 1) {
			if !answered[0x6301+i] {
				t.Fatalf("the Sigma1 on exchange %#x was not answered", 0x6301+i)
			}
		}
		// The handshake ends; the source's next Sigma1 is served.
		end()
		synctest.Wait()
		if !s.IsClosed() || countInbound(m) != 0 {
			t.Fatal("the session outlived its handshake")
		}
		unsecuredRequest(m, 0xAAAA, 0x1000, 0x6400, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		if next := waitHandled(t, calls); next.x.ID() != 0x6400 {
			t.Fatalf("the next handshake ran on exchange %#x", next.x.ID())
		}
	})
}

// s0 is the manager's one inbound unsecured session.
func s0(m *Manager) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.unsecured {
		if s.inbound {
			return s
		}
	}
	return nil
}

// expectBusy fails unless d is a StatusReport Busy (general code 8,
// Secure Channel code 4) whose protocol data is the minimum wait time
// waitMS, little-endian, that acknowledges the Sigma1 it answers.
func expectBusy(t *testing.T, d decoded, waitMS uint16) {
	t.Helper()
	if d.proto.Opcode != mrp.SCOpcodeStatusReport || !d.proto.HasAck || d.proto.Initiator {
		t.Fatalf("answer %+v, want a StatusReport acknowledging the Sigma1", d.proto)
	}
	r, ok := DecodeStatusReport(d.payload)
	if !ok || r.GeneralCode != 8 || r.ProtocolID != uint32(mrp.SecureChannelProtocolID) || r.ProtocolCode != 4 || len(r.ProtocolData) != 2 {
		t.Fatalf("report %+v, want Busy with a 16-bit minimum wait time", r)
	}
	if got := uint16(r.ProtocolData[0]) | uint16(r.ProtocolData[1])<<8; got != waitMS {
		t.Fatalf("minimum wait time %d ms, want %d", got, waitMS)
	}
}

// TestBusyWaitIsTheSigma3Wait: while the running handshake has sent its
// Sigma2 and waits for Sigma3, a refused Sigma1 is told that wait; before
// its Sigma2, 5000 ms; a wait at or beyond 65535 ms is sent as 65535
// (connectedhomeip src/protocols/secure_channel/CASEServer.cpp:99-121 at
// 6170af84; review II, R2-P6-par-2902).
func TestBusyWaitIsTheSigma3Wait(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		opcode uint8
		wait   time.Duration
		want   uint16
	}{
		{"before Sigma2", 0, 0, 5000},
		{"Sigma2_Resume out", mrp.SCOpcodeSigma2Resume, 9 * time.Second, 5000},
		{"Sigma2 out", mrp.SCOpcodeSigma2, 38_250 * time.Millisecond, 38_250},
		{"at the maximum", mrp.SCOpcodeSigma2, 65_535 * time.Millisecond, 65_535},
		{"beyond the maximum", mrp.SCOpcodeSigma2, 2 * time.Minute, 65_535},
	} {
		x := &Exchange{sentOpcode: c.opcode, waitTimeout: c.wait}
		if got := busyWait(x); got != c.want {
			t.Errorf("%s: %d ms, want %d", c.name, got, c.want)
		}
	}
	if got := busyWait(nil); got != 5000 {
		t.Errorf("no running handshake: %d ms, want 5000", got)
	}
}

// TestRefusedSigma1OnSentSigma2IsToldTheWait: a Sigma1 that meets a
// handshake whose handler sent its Sigma2 and waits for Sigma3 is answered
// Busy with that wait; the running handshake continues.
func TestRefusedSigma1OnSentSigma2IsToldTheWait(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		running := make(chan *Exchange, 1)
		got := make(chan *Message, 1)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, _ *Message) {
			running <- x
			if err := x.Send(t.Context(), mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, SendOptions{}); err != nil {
				t.Error(err)
				return
			}
			msg, err := x.Next(t.Context(), ReceiveOptions{ExpectedProcessingTime: 30 * time.Second})
			if err == nil {
				got <- msg
			}
		})
		unsecuredRequest(m, 0xBBBB, 1, 0x6500, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		x := <-running
		sigma2 := decodeUnsecured(t, conn.next(t).bytes)
		if sigma2.proto.Opcode != mrp.SCOpcodeSigma2 {
			t.Fatalf("opcode %#x, want Sigma2", sigma2.proto.Opcode)
		}
		ackHdr := message.Header{MessageCounter: 2, HasSourceNodeID: true, SourceNodeID: 0xBBBB}
		ackProto := message.ProtocolHeader{Initiator: true, HasAck: true, AckCounter: sigma2.hdr.MessageCounter, Opcode: mrp.StandaloneAckOpcode, ExchangeID: 0x6500, ProtocolID: mrp.SecureChannelProtocolID}
		m.HandleDatagram(append(ackHdr.Marshal(), ackProto.Marshal()...), peerAddr)
		synctest.Wait()
		x.mu.Lock()
		wait := x.waitTimeout
		x.mu.Unlock()
		if wait <= 0 {
			t.Fatal("the handler is not waiting for Sigma3")
		}
		unsecuredRequest(m, 0xBBBB, 3, 0x6501, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		busy := decodeUnsecured(t, conn.next(t).bytes)
		expectBusy(t, busy, uint16(min(wait.Milliseconds(), 65535))) //nolint:gosec // clamped
		if busy.proto.ExchangeID != 0x6501 {
			t.Fatalf("Busy on exchange %#x, want 0x6501", busy.proto.ExchangeID)
		}
		// The running handshake continues: its Sigma3 reaches it.
		hdr := message.Header{MessageCounter: 4, HasSourceNodeID: true, SourceNodeID: 0xBBBB}
		proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma3, ExchangeID: 0x6500, ProtocolID: mrp.SecureChannelProtocolID}
		m.HandleDatagram(append(append(hdr.Marshal(), proto.Marshal()...), 0x15, 0x18), peerAddr)
		synctest.Wait()
		select {
		case msg := <-got:
			if msg.Opcode != mrp.SCOpcodeSigma3 {
				t.Fatalf("the running handshake got opcode %#x", msg.Opcode)
			}
		default:
			t.Fatal("the running handshake did not get its Sigma3")
		}
	})
}

// TestSigma1AfterTheHandlerEndedIsServed: a handshake whose handler is done
// with its exchange — closing, its last message still waiting for its
// acknowledgement — no longer holds the session's place: the next Sigma1
// is handled, not answered Busy (the case connectedhomeip's watchdog
// clears, src/protocols/secure_channel/CASEServer.cpp:92-93 at 6170af84;
// review II, R2-P6-par-2902).
func TestSigma1AfterTheHandlerEndedIsServed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		calls := make(chan uint16, 4)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, _ *Message) {
			calls <- x.ID()
			if x.ID() == 0x6600 {
				// Sent, never acknowledged, the wait for it given up: the
				// exchange stays open, closing, for the message's
				// retransmissions.
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
				defer cancel()
				_ = x.Send(ctx, mrp.SCOpcodeStatusReport, StatusReport{GeneralCode: mrp.SCStatusGeneralFailure, ProtocolID: uint32(mrp.SecureChannelProtocolID)}.Encode(), SendOptions{})
			}
		})
		unsecuredRequest(m, 0xCCCC, 1, 0x6600, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		if id := <-calls; id != 0x6600 {
			t.Fatalf("handler on %#x", id)
		}
		_ = conn.next(t)
		time.Sleep(10 * time.Millisecond) // the handler gives up the wait and returns
		synctest.Wait()
		if countInbound(m) != 1 {
			t.Fatal("the ended handshake's session is gone: nothing to test")
		}
		if x := s0(m).heldHandshake(); x == nil || !x.ended() {
			t.Fatal("the session holds no ended handshake")
		}
		unsecuredRequest(m, 0xCCCC, 2, 0x6601, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		synctest.Wait()
		select {
		case id := <-calls:
			if id != 0x6601 {
				t.Fatalf("handler on %#x, want 0x6601", id)
			}
		default:
			t.Fatal("the Sigma1 after the ended handshake was not handled")
		}
	})
}

// TestPeerOpenedHandshakeOnInitiatedSessionIsRefused: a Sigma1, a
// PBKDFParamRequest or an ICD Check-In that names the ephemeral node id of
// an unsecured session this node opened — as destination or as source —
// opens no exchange and starts no handler on it: it is acknowledged and
// dropped, and the session and its own handshake stay. Only a session a
// peer's initiating message opened takes such a message (review II,
// R2-P6-sec-1801, R2-P6-bound-1802, owner decision 189). Before, the
// Sigma1 reached the CASE responder, whose return closed the initiator's
// session, and every PBKDFParamRequest with a new exchange id started a
// refusal on a goroutine outside every bound of the inbound sessions.
func TestPeerOpenedHandshakeOnInitiatedSessionIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		opcode uint8
	}{
		{"Sigma1", mrp.SCOpcodeSigma1},
		{"PBKDFParamRequest", mrp.SCOpcodePBKDFParamRequest},
		{"Check-In", mrp.SCOpcodeIcdCheckIn},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				m, conn, _ := newTestManager(t, Config{})
				h, calls := recordingHandler(nil)
				m.SetProtocolHandler(mrp.SecureChannelProtocolID, h)
				m.SetCheckInReceiver(func([]byte) bool { t.Error("the Check-In receiver ran"); return true })
				s, err := m.NewUnsecuredSession(peerAddr, nil)
				if err != nil {
					t.Fatal(err)
				}
				own, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
				if err != nil {
					t.Fatal(err)
				}
				const n = 8
				for i := range uint16(n) {
					hdr := message.Header{MessageCounter: uint32(i) + 1}
					if i%2 == 0 {
						hdr.DestSize, hdr.DestNodeID = message.DestNodeID, s.NodeID()
					} else {
						hdr.HasSourceNodeID, hdr.SourceNodeID = true, s.NodeID()
					}
					proto := message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: c.opcode, ExchangeID: 0x7700 + i, ProtocolID: mrp.SecureChannelProtocolID}
					m.HandleDatagram(append(append(hdr.Marshal(), proto.Marshal()...), 0x15, 0x18), peerAddr)
				}
				synctest.Wait()
				if len(calls) != 0 {
					t.Fatalf("%d handlers ran on the initiated session", len(calls))
				}
				m.mu.Lock()
				held := len(m.exchanges)
				m.mu.Unlock()
				if held != 1 || s.IsClosed() || s.IsClosing() {
					t.Fatalf("%d exchanges held, session closed %v closing %v; want the own exchange only, session open", held, s.IsClosed(), s.IsClosing())
				}
				for i := range uint16(n) {
					ack := decodeUnsecured(t, conn.next(t).bytes)
					if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.ExchangeID != 0x7700+i {
						t.Fatalf("datagram %d: answer %+v, want its acknowledgement", i, ack.proto)
					}
				}
				conn.none(t)
				own.Close()
				s.Close()
			})
		})
	}
}
