// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestExchangesAreKeyedBySession: two peers that open an exchange with the
// same id each keep their own; a peer's next message reaches its exchange,
// not a new handler. Mirrors matter.js ExchangeManager.ts:103 (#exchanges,
// a map per session) and :401 (the lookup by session and index) at
// db021da0 (review II, R2-P2-ctr-01).
func TestExchangesAreKeyedBySession(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		sA, pA := newSecurePair(t, m, 0x0401, false)
		sB, pB := newSecurePair(t, m, 0x0402, false)
		release := make(chan struct{})
		defer close(release)
		h, calls := recordingHandler(release)
		m.SetProtocolHandler(im.InteractionModelProtocolID, h)
		pA.send(t, sA.ID(), peerOpens(2, im.InteractionModelProtocolID, im.OpcodeReportData, true), []byte{0x15, 0x18})
		a := waitHandled(t, calls)
		pB.send(t, sB.ID(), peerOpens(2, im.InteractionModelProtocolID, im.OpcodeReportData, true), []byte{0x15, 0x18})
		if b := waitHandled(t, calls); b.x.Session() != sB || b.x == a.x {
			t.Fatal("B's exchange")
		}
		pA.send(t, sA.ID(), peerOpens(2, im.InteractionModelProtocolID, im.OpcodeStatusResponse, true), []byte{0x15, 0x18})
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if msg, err := a.x.Next(ctx, ReceiveOptions{Timeout: time.Hour}); err != nil || msg.Opcode != im.OpcodeStatusResponse {
			t.Fatalf("A's second message on A's exchange: %v, %v", msg, err)
		}
		select {
		case c := <-calls:
			t.Fatalf("A's second message opened a new exchange on session %#x", c.x.Session().ID())
		default:
		}
	})
}

// TestInitiateSkipsAnIDInUseOnTheSession: an exchange id an open exchange
// this node started on the session still holds is skipped; another
// session may use it. Mirrors matter.js ExchangeManager.ts:189-199
// (#freeExchangeIdFor) at db021da0 (review II, R2-P2-ctr-01).
func TestInitiateSkipsAnIDInUseOnTheSession(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	sA, _ := newSecurePair(t, m, 0x0401, false)
	sB, _ := newSecurePair(t, m, 0x0402, false)
	first, err := m.Initiate(sA, im.InteractionModelProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	rewind := func() {
		m.mu.Lock()
		m.exchangeID = first.ID() - 1
		m.mu.Unlock()
	}
	rewind()
	again, err := m.Initiate(sA, im.InteractionModelProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.ID() == first.ID() {
		t.Fatalf("exchange id %d reused on the same session", again.ID())
	}
	rewind()
	other, err := m.Initiate(sB, im.InteractionModelProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.ID() != first.ID() {
		t.Fatalf("another session got id %d, want %d", other.ID(), first.ID())
	}
}

// TestNoExchangeRegistersOnAClosedSession: an exchange that reaches a
// session after its close took the snapshot of the exchanges it aborts
// is not registered — not in the manager's table, not on the session —
// so it can neither outlive the session nor wait in Next without a
// deadline (review II, R2-P6-conc-1809). The accept path is driven on a
// closed session directly; Initiate is raced against Close, and
// afterwards no exchange of a closed session is left in the table.
func TestNoExchangeRegistersOnAClosedSession(t *testing.T) {
	t.Parallel()
	t.Run("accept", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		s, _ := newSecurePair(t, m, 0x0501, false)
		s.Close()
		if x := m.accept(s, &Message{ExchangeID: 3, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReportData, Initiator: true}); x != nil {
			t.Fatal("an exchange was accepted on a closed session")
		}
		if n := tableSize(m); n != 0 || len(s.liveExchanges()) != 0 {
			t.Fatalf("%d exchanges in the table, %d on the session", n, len(s.liveExchanges()))
		}
		u := &Session{m: m, inbound: true, exchanges: map[*Exchange]struct{}{}, closing: make(chan struct{})}
		u.Close()
		if x := m.accept(u, &Message{ExchangeID: 4, ProtocolID: mrp.SecureChannelProtocolID, Opcode: mrp.SCOpcodeSigma1, Initiator: true}); x != nil {
			t.Fatal("a handshake was accepted on a closed session")
		}
		if n := tableSize(m); n != 0 {
			t.Fatalf("%d exchanges in the table", n)
		}
	})
	t.Run("Initiate against Close", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		for range 200 {
			s, err := m.NewUnsecuredSession(peerAddr, nil)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { s.Close(); close(done) }()
			x, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
			<-done
			if err == nil {
				x.Close()
			} else if !errors.Is(err, ErrSessionClosed) {
				t.Fatal(err)
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for k, x := range m.exchanges {
			if k.s.IsClosed() && !x.considerClosed() {
				t.Fatalf("exchange %d of a closed session left open in the table", x.id)
			}
		}
	})
}

func tableSize(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.exchanges)
}
