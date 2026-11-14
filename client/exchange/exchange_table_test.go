// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/im"
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
