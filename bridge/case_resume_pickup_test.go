// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestResumePickupFailureEndsTheAdapter: a resume whose session the host
// cannot install — its stake lost to another handshake, say — ends the
// adapter's handshake as a failed Sigma1 does: the adapter leaves its
// provider (the eviction hook runs) and returns the id it staked, instead
// of staying until the reaper takes it (review II, R2-P6-sec-0301 round 2,
// Fable read rows 7 and 20).
func TestResumePickupFailureEndsTheAdapter(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	respID := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	sigma1Bytes, secret, rid := buildResumeSigma1(t)
	var released []uint16
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		responder := sigma.NewResponder(respID, caseTestVerifier{}, 0x2001)
		responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return errors.New("stake lost") })
		a.SetOnUnfinished(func(id uint16) { released = append(released, id) })
		return a
	})
	var evicted []HandshakeKey
	p.SetOnEvict(func(k HandshakeKey) { evicted = append(evicted, k) })
	k := HandshakeKey{InitiatorNodeID: 0x1000, ExchangeID: 1}
	a, ok := p.Resolve(k).(*CaseAdapter)
	if !ok {
		t.Fatal("no adapter")
	}
	if _, _, err := a.ProcessSigma1(sigma1Bytes); err == nil {
		t.Fatal("the resume's pickup error did not come back")
	}
	if p.Active() != 0 || len(evicted) != 1 || evicted[0] != k {
		t.Fatalf("after a failed resume pickup: %d adapters, evictions %v; want the adapter gone and its eviction reported", p.Active(), evicted)
	}
	if len(released) != 1 || released[0] != 0x2001 {
		t.Fatalf("staked ids released %v, want [0x2001]", released)
	}
}
