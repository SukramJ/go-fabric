// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestReplayedSigma1UnderOtherKeysEndsNoHandshake: a Sigma1 is sent in the
// clear and binds nothing to the header's source node id or exchange id, so
// a node on the link can send a captured one again under any number of
// other keys. The bytes of a Sigma1 the bridge holds a record of are
// acknowledged and not judged under another key: the copies begin no
// handshake, take no record's or adapter's place, and the handshake the
// Sigma1 came from completes (review II, R2-P6-sec-0301, N4-f3-2).
func TestReplayedSigma1UnderOtherKeysEndsNoHandshake(t *testing.T) {
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the tests' encoding of the root key
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	established := 0
	next := uint16(0x2001)
	provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
		responder := sigma.NewResponder(ours, caseTestVerifier{}, next)
		next++
		responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { established++; return nil })
		return a
	})
	provider.SetOnEvict(r.b.ForgetSigma1Replied)
	r.b.AttachCaseHandlerProvider(provider.Resolve)

	victim := newPipelinePeer(t, r.b, 0x1111)
	ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1002, PeerNodeID: 0xBBBB, RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := ini.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	if err := victim.send(mrp.SCOpcodeSigma1, 0x0300, s1, nil); err != nil {
		t.Fatal(err)
	}
	s2, c2, _ := victim.recv(mrp.SCOpcodeSigma2, "Sigma2")
	s3, err := ini.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}

	attacker := newPipelinePeer(t, r.b, 0)
	for i := range maxCaseHandshakes {
		attacker.nodeID = uint64(0x66600000 + i)
		if err := attacker.send(mrp.SCOpcodeSigma1, uint16(0x4000+i), s1, nil); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	attacker.none(300*time.Millisecond, "a captured Sigma1 sent again under other keys")
	r.b.mu.RLock()
	records := len(r.b.sigma1Replied)
	r.b.mu.RUnlock()
	if records != 1 || provider.Active() != 1 {
		t.Fatalf("after %d replays: %d records, %d adapters; want the victim's alone", maxCaseHandshakes, records, provider.Active())
	}
	if err := victim.send(mrp.SCOpcodeSigma3, 0x0300, s3, &c2); err != nil {
		t.Fatalf("the victim's Sigma3: %v", err)
	}
	victim.recv(mrp.SCOpcodeStatusReport, "the victim's success report")
	if established != 1 {
		t.Fatalf("sessions established for the victim: %d, want 1", established)
	}
}

// TestCaseProviderEvictsOnlyForAHandshakeThatBegan: a provider at its bound
// judges a new Sigma1 on an adapter that takes nobody's place; only once
// that Sigma1 began a handshake does the least recently used adapter go,
// and an adapter whose Sigma1 began nothing leaves without evicting
// anybody (review II, R2-P6-sec-0301).
func TestCaseProviderEvictsOnlyForAHandshakeThatBegan(t *testing.T) {
	t.Parallel()
	var evicted []HandshakeKey
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		return NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))
	})
	p.SetOnEvict(func(k HandshakeKey) { evicted = append(evicted, k) })
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	p.SetClock(clk)
	key := func(i int) HandshakeKey { return HandshakeKey{InitiatorNodeID: uint64(0x1000 + i), ExchangeID: 1} }
	for i := range maxCaseHandshakes {
		clk.Advance(time.Millisecond)
		p.Resolve(key(i)).(*CaseAdapter).sigma1BeganHandshake()
	}
	// A Sigma1 that begins nothing: its adapter is judged and leaves.
	clk.Advance(time.Millisecond)
	refused := p.Resolve(key(maxCaseHandshakes)).(*CaseAdapter)
	if len(evicted) != 0 {
		t.Fatalf("an adapter being judged evicted %v", evicted)
	}
	refused.handshakeFailed()
	if got := p.Active(); got != maxCaseHandshakes || len(evicted) != 1 || evicted[0] != key(maxCaseHandshakes) {
		t.Fatalf("after a Sigma1 that began nothing: %d adapters, evictions %v; want %d and only its own", got, evicted, maxCaseHandshakes)
	}
	// One that begins a handshake takes the least recently used place.
	clk.Advance(time.Millisecond)
	p.Resolve(key(maxCaseHandshakes + 1)).(*CaseAdapter).sigma1BeganHandshake()
	if got := p.Active(); got != maxCaseHandshakes || len(evicted) != 2 || evicted[1] != key(0) {
		t.Fatalf("after a Sigma1 that began a handshake: %d adapters, evictions %v; want %d and the oldest gone", got, evicted, maxCaseHandshakes)
	}
}

// TestCaseProviderLifetimeRunsFromTheHandshake: an adapter's lifetime runs
// from the Sigma1 that began its handshake; a later Resolve of its key — a
// Sigma1 on a completed key that begins nothing — does not extend it, so
// repeated ones do not keep the record and its window alive (review II,
// U4-fr-sec-1).
func TestCaseProviderLifetimeRunsFromTheHandshake(t *testing.T) {
	t.Parallel()
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		return NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))
	})
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	p.SetClock(clk)
	k := HandshakeKey{InitiatorNodeID: 0x1000, ExchangeID: 1}
	p.Resolve(k).(*CaseAdapter).sigma1BeganHandshake()
	for range 5 {
		clk.Advance(20 * time.Second)
		p.Resolve(k)
	}
	p.reapLocked(time.Minute)
	if got := p.Active(); got != 0 {
		t.Fatalf("an adapter Resolve kept for 100 s past its handshake's start survived a 60 s reap: %d adapters", got)
	}
}

// TestCaseProviderForgetReportsTheEviction: Forget ends the handshake's
// state outside the provider as the reaper does — the eviction hook runs —
// so the router's record, its unsecured window and a connection's hold go
// with the adapter (review II, N3-findev-2).
func TestCaseProviderForgetReportsTheEviction(t *testing.T) {
	t.Parallel()
	var evicted []HandshakeKey
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		return NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))
	})
	p.SetOnEvict(func(k HandshakeKey) { evicted = append(evicted, k) })
	k := HandshakeKey{InitiatorNodeID: 0x1000, ExchangeID: 1}
	p.Resolve(k)
	p.Forget(k)
	p.Forget(k) // nothing held: nothing reported
	if len(evicted) != 1 || evicted[0] != k {
		t.Fatalf("evictions reported by Forget: %v, want %v once", evicted, k)
	}
}
