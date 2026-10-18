// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/ExchangeManagerTest.ts
// at 862ad75f: peer loss conveyed by and spared for the failing exchange, a
// duplicate as peer activity, and inbound unsecured sessions no handler
// adopts. The subject is the controller's Manager; matter.js's session
// subscriptions have no Go counterpart, so "conveys the failing exchange" is
// held as the session sparing that exchange and closing once it ends.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// emFailingPeer is failingPeer (ExchangeManagerTest.ts:94-142): a fabric
// session whose every send fails, created a second before the exchange.
func emFailingPeer(t *testing.T, suppress bool) (*Manager, *Session, *lossRecorder, *fakeClock) {
	t.Helper()
	rec := &lossRecorder{}
	m, conn, clock := newTestManager(t, Config{PeerLost: rec.hook})
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID: 0x0400, Addr: peerAddr, Channel: newSecureConfig(),
		PeerParameters: PeerParameters(nil), FabricIndex: 1, SuppressPeerLoss: suppress,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn.fail = errors.New("simulated network failure")
	clock.Advance(time.Second)
	return m, s, rec, clock
}

func TestExchangeManagerPeerLoss(t *testing.T) {
	t.Parallel()
	t.Run("conveys the failing exchange so its own subscription does not wait on itself", func(t *testing.T) {
		m, s, rec, clock := emFailingPeer(t, false)
		x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
		if err := x.Send(context.Background(), 0, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
			t.Fatalf("Send = %v", err)
		}
		if !s.IsPeerLost() || rec.count(s) != 1 {
			t.Fatalf("lost %v, hooks %d", s.IsPeerLost(), rec.count(s))
		}
		select {
		case <-x.Done():
			t.Fatal("the failing exchange was closed by its own peer loss")
		default:
		}
		x.Close() // the unacknowledged message keeps it open for its retransmission window
		clock.Advance(time.Hour)
		<-x.Done()
		if !s.IsClosed() {
			t.Error("session not closed once the failing exchange ended")
		}
	})
	for _, tc := range []struct {
		name            string
		sessionSuppress bool
		exchangeOpts    Options
	}{
		{"leaves the session and its subscriptions alone when the exchange suppresses peer loss", false, Options{SuppressPeerLoss: true}},
		{"leaves a session alone that suppresses peer loss", true, Options{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, rec, _ := emFailingPeer(t, tc.sessionSuppress)
			x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, tc.exchangeOpts)
			if err := x.Send(context.Background(), 0, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
				t.Fatalf("Send = %v", err)
			}
			if rec.count(s) != 0 || s.IsPeerLost() || s.IsClosed() {
				t.Errorf("hooks %d, lost %v, closed %v", rec.count(s), s.IsPeerLost(), s.IsClosed())
			}
			select {
			case <-s.Closing():
				t.Error("session closing")
			default:
			}
		})
	}
}

func TestExchangeManagerInboundSecureMessages(t *testing.T) {
	t.Parallel()
	t.Run("counts a duplicate message as peer activity", func(t *testing.T) {
		m, _, clock := newTestManager(t, Config{})
		s, peer := newSecurePair(t, m, 0x0410, false)
		// An unsolicited standalone ack (ExchangeManagerTest.ts:460-470).
		ack := peer.send(t, s.ID(), message.ProtocolHeader{
			Opcode: mrp.StandaloneAckOpcode, ExchangeID: 0x1234, ProtocolID: mrp.SecureChannelProtocolID,
			HasAck: true, AckCounter: 0x5678,
		}, nil)
		first := s.ActiveTimestamp()
		clock.Advance(time.Second)
		m.HandleDatagram(ack, peerAddr) // the same datagram again: a duplicate
		if got := s.ActiveTimestamp(); !got.After(first) {
			t.Errorf("active timestamp %v not after %v", got, first)
		}
	})
}

// emUnsecured is unsecuredMessage (ExchangeManagerTest.ts:566-589).
func emUnsecured(src uint64, counter uint32, opcode uint8, requiresAck bool) []byte {
	hdr := message.Header{MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: src}
	proto := message.ProtocolHeader{Initiator: true, NeedsAck: requiresAck, Opcode: opcode, ExchangeID: 1, ProtocolID: mrp.SecureChannelProtocolID}
	return append(hdr.Marshal(), proto.Marshal()...)
}

func emUnsecuredCount(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.unsecured)
}

func TestExchangeManagerInboundUnsecuredSessions(t *testing.T) {
	t.Parallel()
	t.Run("discards the session when the handler does not adopt it", func(t *testing.T) {
		// The controller hands an unsecured session's Sigma1 to the Secure
		// Channel handler; it answers nothing and closes the exchange, as
		// silentHandler does (ExchangeManagerTest.ts:555-564).
		m, _, clock := newTestManager(t, Config{})
		handled := make(chan struct{}, 4)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, _ *Message) {
			x.Close()
			handled <- struct{}{}
		})
		for i := range 3 {
			m.HandleDatagram(emUnsecured(uint64(i+1), uint32(i+1), mrp.SCOpcodeSigma1, false), peerAddr)
			<-handled
		}
		for i := 0; i < 1000 && emUnsecuredCount(m) != 0; i++ {
			clock.Yield()
		}
		if n := emUnsecuredCount(m); n != 0 {
			t.Errorf("%d unsecured sessions left", n)
		}
	})
	t.Run("discards the session when no handler is registered", func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		m.HandleDatagram(emUnsecured(1, 1, 0x50, true), peerAddr) // IcdCheckInMessage
		if n := emUnsecuredCount(m); n != 0 {
			t.Errorf("%d unsecured sessions left", n)
		}
	})
}
