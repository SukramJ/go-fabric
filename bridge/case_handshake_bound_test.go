// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// sigma1From is the header of a Sigma1 of initiator nodeID.
func sigma1From(nodeID uint64) *message.Header {
	return &message.Header{SessionID: 0, MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: nodeID}
}

// TestSigma1RecordsAreBounded: the records of the Sigma1 the bridge
// answered are held for at most maxCaseHandshakes handshakes, the newest;
// a record that loses its place is the oldest (review II, R2-P3-dr-dev-03).
func TestSigma1RecordsAreBounded(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachCaseHandler(&recordingCaseHandler{sigma1RespCode: mrp.SCOpcodeSigma2, sigma1Resp: []byte{0xDD}})
	const extra = 8
	for i := range maxCaseHandshakes + extra {
		if err := b.dispatchSecureChannel(loopbackSrc(), sigma1From(uint64(0x1000+i)), scProto(mrp.SCOpcodeSigma1, 1, false, 0), []byte{0x01}); err != nil {
			t.Fatalf("Sigma1 %d: %v", i, err)
		}
	}
	b.mu.RLock()
	held := len(b.sigma1Replied)
	_, oldest := b.sigma1Replied[HandshakeKey{InitiatorNodeID: 0x1000, ExchangeID: 1}]
	_, newest := b.sigma1Replied[HandshakeKey{InitiatorNodeID: uint64(0x1000 + maxCaseHandshakes + extra - 1), ExchangeID: 1}]
	b.mu.RUnlock()
	if held > maxCaseHandshakes {
		t.Fatalf("the bridge holds %d Sigma1 records, want at most %d", held, maxCaseHandshakes)
	}
	if oldest || !newest {
		t.Fatalf("the oldest record held: %v, the newest: %v; want the oldest to have lost its place", oldest, newest)
	}
}

// TestCaseAdaptersAreBounded: a PerExchangeCaseProvider holds at most
// maxCaseHandshakes adapters; the one that loses its place for a new
// handshake is the least recently used, and it goes as a reaped one goes —
// its eviction is reported and its staked session id released (review II,
// R2-P3-dr-dev-03).
func TestCaseAdaptersAreBounded(t *testing.T) {
	t.Parallel()
	var released atomic.Int32
	var evicted []HandshakeKey
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		a := NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))
		a.SetOnUnfinished(func(uint16) { released.Add(1) })
		return a
	})
	p.SetOnEvict(func(k HandshakeKey) { evicted = append(evicted, k) })
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	p.SetClock(clk)
	const extra = 8
	for i := range maxCaseHandshakes + extra {
		clk.Advance(time.Millisecond)
		if p.Resolve(HandshakeKey{InitiatorNodeID: uint64(0x1000 + i), ExchangeID: 1}) == nil {
			t.Fatalf("Resolve %d: no adapter", i)
		}
	}
	if got := p.Active(); got != maxCaseHandshakes {
		t.Fatalf("the provider holds %d adapters, want %d", got, maxCaseHandshakes)
	}
	if len(evicted) != extra || evicted[0] != (HandshakeKey{InitiatorNodeID: 0x1000, ExchangeID: 1}) {
		t.Fatalf("evictions reported %v, want the %d oldest, the first one first", evicted, extra)
	}
	if got := released.Load(); got != extra {
		t.Fatalf("%d staked session ids released, want %d", got, extra)
	}
}

// TestSigma1WithoutHandshakeStateLeavesNoRecord: a Sigma1 whose handling
// left no handshake behind — the handler failed it, no handler is
// wired, or an adapter outside a provider answered it with a failure
// report — leaves no Sigma1 record (review II, R2-P3-dr-dev-03).
func TestSigma1WithoutHandshakeStateLeavesNoRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler CaseHandler
	}{
		{"the handler fails the Sigma1", &recordingCaseHandler{sigma1Err: errors.New("test: refused")}},
		{"no handler is wired", nil},
		{"an adapter outside a provider answers with a failure report", NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			if tc.handler != nil {
				b.AttachCaseHandler(tc.handler)
			}
			_ = b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodeSigma1, 5, false, 0), []byte{0x01})
			b.mu.RLock()
			_, held := b.sigma1Replied[scKey(5)]
			b.mu.RUnlock()
			if held {
				t.Fatal("a Sigma1 that left no handshake state left its record")
			}
		})
	}
}
