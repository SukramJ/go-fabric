// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// A handshake's session id is staked when its adapter is built
// (node/security.go); a message that starts no handshake builds none, and an
// adapter whose handshake does not finish returns its id (review II,
// R2-P3-lan-dev-02). matter.js routes only a PBKDFParamRequest or a Sigma1
// that opens an exchange to its PASE and CASE servers
// (packages/protocol/src/securechannel/SecureChannelProtocol.ts:129, :146 at
// db021da0) and reserves no id before the session
// (SessionManager.ts:581-598).
//
// Such a message is acknowledged and no more: no StatusReport, no counted
// PASE failure (review II, R2-P4-cert-02, owner decision 70). matter.js hands
// every other opening message to the base handler, which throws
// (SecureChannelProtocol.ts:152-153, :43-50 at db021da0), and the exchange
// manager only logs the error (ExchangeManager.ts:494, :538-549); chip has
// no unsolicited handler for the type and sends the standalone
// acknowledgement only (src/messaging/ExchangeMgr.cpp:437 at 6170af84).

// requireAcknowledgedOnly fails t unless the message on exchangeID was
// acknowledged and no more: its acknowledgement is still owed — no reply
// carried it — and nothing on the exchange waits for the peer's.
func requireAcknowledgedOnly(t *testing.T, b *Bridge, exchangeID uint16, what string) {
	t.Helper()
	if !b.ackTracker.Owes(0, exchangeID, false) {
		t.Fatalf("%s on exchange %#x: no acknowledgement owed — a reply carried it", what, exchangeID)
	}
	if got := pendingOnUnsecuredExchange(b, exchangeID); got != 0 {
		t.Fatalf("%s on exchange %#x: %d reliable replies sent, want none", what, exchangeID, got)
	}
}

func TestCASEMessagesThatStartNoHandshakeBuildNoAdapter(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	var built atomic.Int32
	provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
		built.Add(1)
		return NewCaseAdapter(sigma.NewResponder(nil, caseTestVerifier{}, 1))
	})
	r.b.AttachCaseHandlerProvider(provider.Resolve)
	peer := newPipelinePeer(t, r.b, 0xBAD0BAD0)
	for i := range 32 {
		_ = peer.send(mrp.SCOpcodeSigma3, uint16(0x4000+i), []byte{0x15, 0x18}, nil)
		_ = peer.send(mrp.SCOpcodeSigma2Resume, uint16(0x5000+i), []byte{0x15, 0x18}, nil)
	}
	if got := built.Load(); got != 0 {
		t.Fatalf("CASE adapters built for Sigma3 / Sigma2_Resume on exchanges without a Sigma1: %d, want 0", got)
	}
	for i := range 32 {
		requireAcknowledgedOnly(t, r.b, uint16(0x4000+i), "a Sigma3 without a Sigma1")
		requireAcknowledgedOnly(t, r.b, uint16(0x5000+i), "a Sigma2_Resume without a Sigma1")
	}
}

func TestPASEMessagesThatStartNoHandshakeBuildNoAdapter(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	var built atomic.Int32
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		built.Add(1)
		return newPaseAdapterWithVerifier(t)
	})
	r.b.AttachPaseHandlerProvider(provider.Resolve)
	peer := newPipelinePeer(t, r.b, 0xBAD0BAD0)
	_ = peer.send(mrp.SCOpcodePake1, 0x4000, []byte{0x15, 0x18}, nil)
	_ = peer.send(mrp.SCOpcodePake3, 0x4001, []byte{0x15, 0x18}, nil)
	if got := built.Load(); got != 0 {
		t.Fatalf("PASE adapters built for Pake1 / Pake3 without a PBKDFParamRequest: %d, want 0", got)
	}
	requireAcknowledgedOnly(t, r.b, 0x4000, "a Pake1 without a PBKDFParamRequest")
	requireAcknowledgedOnly(t, r.b, 0x4001, "a Pake3 without a PBKDFParamRequest")
	if got := r.b.failedAttempts.Load(); got != 0 {
		t.Fatalf("PASE failures = %d, want 0: no pairing began", got)
	}
}

// TestPASEContinuationsThatStartNoHandshakeAreDroppedOnTheSingleton: with
// one adapter wired through AttachPaseHandler and no provider, a Pake1 or
// Pake3 on an exchange no PBKDFParamRequest began is acknowledged and no
// more, as on the provider path: the adapter is not handed it, no
// StatusReport, no counted PASE failure (review II, N5-rw2-1). With another
// exchange's handshake holding the claim, a Pake1 stays unanswered and
// uncounted as well (the in-progress gate of dispatchPase).
func TestPASEContinuationsThatStartNoHandshakeAreDroppedOnTheSingleton(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	var made atomic.Int32
	factory := newVerifierFactory(t, &made)
	a := NewPaseAdapterWithFactory(factory)
	r.b.AttachPaseHandler(a)
	peer := newPipelinePeer(t, r.b, 0xBAD0BAD0)
	_ = peer.send(mrp.SCOpcodePake1, 0x4000, []byte{0x15, 0x18}, nil)
	_ = peer.send(mrp.SCOpcodePake3, 0x4001, []byte{0x15, 0x18}, nil)
	requireAcknowledgedOnly(t, r.b, 0x4000, "a Pake1 without a PBKDFParamRequest")
	requireAcknowledgedOnly(t, r.b, 0x4001, "a Pake3 without a PBKDFParamRequest")
	if got := r.b.failedAttempts.Load(); got != 0 {
		t.Fatalf("PASE failures = %d, want 0: no pairing began", got)
	}
	if got := made.Load(); got != 0 {
		t.Fatalf("verifiers made = %d, want 0: the adapter ran a Pake1 no PBKDFParamRequest began", got)
	}
	// Another exchange's handshake holds the claim.
	r.b.claimPaseInFlight(HandshakeKey{InitiatorNodeID: 0x0B0B, ExchangeID: 0x4100})
	_ = peer.send(mrp.SCOpcodePake1, 0x4002, []byte{0x15, 0x18}, nil)
	requireAcknowledgedOnly(t, r.b, 0x4002, "a Pake1 beside another exchange's handshake")
	if got := r.b.failedAttempts.Load(); got != 0 {
		t.Fatalf("PASE failures = %d, want 0: the Pake1 began no pairing", got)
	}
}

// releaseRecorder records the session ids an adapter returns.
type releaseRecorder struct {
	mu  sync.Mutex
	ids []uint16
}

func (r *releaseRecorder) release(id uint16) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
}

func (r *releaseRecorder) released() []uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint16(nil), r.ids...)
}

func TestCASEAdapterReturnsItsSessionIDWhenItsHandshakeDoesNotFinish(t *testing.T) {
	t.Parallel()
	t.Run("a failed Sigma1", func(t *testing.T) {
		t.Parallel()
		var rec releaseRecorder
		provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
			a := NewCaseAdapter(sigma.NewResponder(nil, caseTestVerifier{}, 0x2001))
			a.SetOnUnfinished(rec.release)
			return a
		})
		var evicted []HandshakeKey
		provider.SetOnEvict(func(key HandshakeKey) { evicted = append(evicted, key) })
		key := HandshakeKey{InitiatorNodeID: 0xC0DE, ExchangeID: 7}
		opcode, _, err := provider.Resolve(key).ProcessSigma1([]byte{0x15, 0x18})
		if err != nil || opcode != mrp.SCOpcodeStatusReport {
			t.Fatalf("ProcessSigma1 of a malformed Sigma1 = (%#x, %v), want a failure StatusReport", opcode, err)
		}
		if got := rec.released(); len(got) != 1 || got[0] != 0x2001 {
			t.Fatalf("session ids returned = %v, want [0x2001]", got)
		}
		if provider.Active() != 0 || len(evicted) != 1 || evicted[0] != key {
			t.Fatalf("the ended handshake's adapter stays: active %d, evicted %v", provider.Active(), evicted)
		}
	})
	t.Run("an adapter its provider drops", func(t *testing.T) {
		t.Parallel()
		var rec releaseRecorder
		provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
			a := NewCaseAdapter(sigma.NewResponder(nil, caseTestVerifier{}, 0x2002))
			a.SetOnUnfinished(rec.release)
			return a
		})
		key := HandshakeKey{InitiatorNodeID: 0xC0DE, ExchangeID: 8}
		provider.Resolve(key)
		provider.Forget(key)
		provider.Forget(key)
		if got := rec.released(); len(got) != 1 || got[0] != 0x2002 {
			t.Fatalf("session ids returned = %v, want [0x2002] once", got)
		}
	})
	t.Run("an established session keeps its id", func(t *testing.T) {
		t.Parallel()
		var rec releaseRecorder
		a := NewCaseAdapter(sigma.NewResponder(nil, caseTestVerifier{}, 0x2003))
		a.SetOnUnfinished(rec.release)
		a.mu.Lock()
		a.established, a.establishedSessionID = true, 0x2003
		a.mu.Unlock()
		provider := NewPerExchangeCaseProvider(func() *CaseAdapter { return a })
		key := HandshakeKey{InitiatorNodeID: 0xC0DE, ExchangeID: 9}
		provider.Resolve(key)
		provider.Reset()
		if got := rec.released(); len(got) != 0 {
			t.Fatalf("session ids returned = %v, want none: the id names an established session", got)
		}
	})
}

func TestPASEAdapterReturnsItsSessionIDWhenItsProviderDropsIt(t *testing.T) {
	t.Parallel()
	var rec releaseRecorder
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 0x3001)
		a.SetOnUnfinished(rec.release)
		return a
	})
	key := HandshakeKey{InitiatorNodeID: 0xC0FFEE, ExchangeID: 7}
	provider.Resolve(key)
	provider.Forget(key)
	provider.Resolve(key)
	provider.Reset()
	if got := rec.released(); len(got) != 2 || got[0] != 0x3001 || got[1] != 0x3001 {
		t.Fatalf("session ids returned = %v, want [0x3001 0x3001] (one per dropped adapter)", got)
	}
}
