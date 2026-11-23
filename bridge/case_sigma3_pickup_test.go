// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/secure/sigma"
)

// TestSigma3PickupFailureEndsTheAdapter: a Sigma3 the responder verified
// whose session the host cannot install ends the adapter's handshake as
// the resume arm does (TestResumePickupFailureEndsTheAdapter): the adapter
// is not left established, it leaves its provider (the eviction hook
// runs) and returns the id it staked once (review II, R2-C2-fsm-0401).
func TestSigma3PickupFailureEndsTheAdapter(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	initID := newCaseTestIdentity(t, 0xAAAA, 1, ipk)
	respID := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	initiator := sigma.NewInitiator(initID, caseTestVerifier{}, 0x1001, [32]byte{0xDE, 0xAD, 0xBE, 0xEF})
	var released []uint16
	p := NewPerExchangeCaseProvider(func() *CaseAdapter {
		a := NewCaseAdapter(sigma.NewResponder(respID, caseTestVerifier{}, 0x2001))
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return errors.New("table refused") })
		a.SetOnUnfinished(func(id uint16) { released = append(released, id) })
		return a
	})
	var evicted []HandshakeKey
	p.SetOnEvict(func(k HandshakeKey) { evicted = append(evicted, k) })
	k := HandshakeKey{InitiatorNodeID: 0xAAAA, ExchangeID: 1}
	a, ok := p.Resolve(k).(*CaseAdapter)
	if !ok {
		t.Fatal("no adapter")
	}
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := a.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ProcessSigma3(s3); err == nil {
		t.Fatal("the Sigma3 pickup error did not come back")
	}
	if _, est := a.establishedSession(); est {
		t.Error("the adapter reads established after a failed Sigma3 pickup")
	}
	if p.Active() != 0 || len(evicted) != 1 || evicted[0] != k {
		t.Errorf("after a failed Sigma3 pickup: %d adapters, evictions %v; want the adapter gone and its eviction reported", p.Active(), evicted)
	}
	if len(released) != 1 || released[0] != 0x2001 {
		t.Errorf("staked ids released %v, want [0x2001]", released)
	}
	a.releaseStakedID() // a later drop by the provider returns nothing twice
	if len(released) != 1 {
		t.Errorf("staked ids released %v after a second release, want one", released)
	}
}

// TestCaseSetOnSessionEstablishedBesideSigma3 installs the callback while
// a Sigma3 runs; under -race the setter and the handshake's read of the
// callback must not race (review II, R2-C2-conc-0402).
func TestCaseSetOnSessionEstablishedBesideSigma3(t *testing.T) {
	t.Parallel()
	a, initiator, _ := pairedCaseAdapter(t)
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := a.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 1000 {
			a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return nil })
		}
	})
	_, _, _, _, _ = a.processSigma3(s3)
	wg.Wait()
}
