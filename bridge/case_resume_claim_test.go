// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// claimingResumptionStore serves resumption records and claims their ids,
// as the node's store does ([sigma.Sigma1ProcessResult.Claim]).
type claimingResumptionStore struct {
	mu      sync.Mutex
	secrets map[string][]byte
	held    map[string]bool
}

func (s *claimingResumptionStore) GetByID(id []byte) (*sigma.ResumptionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[string(id)]
	if !ok {
		return nil, errors.New("claimingResumptionStore: not found")
	}
	return &sigma.ResumptionRecord{SharedSecret: append([]byte(nil), secret...), ResumptionID: append([]byte(nil), id...)}, nil
}

func (s *claimingResumptionStore) ClaimResumptionID(id []byte) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[string(id)] {
		return nil, false
	}
	s.held[string(id)] = true
	return func() { s.mu.Lock(); delete(s.held, string(id)); s.mu.Unlock() }, true
}

func (s *claimingResumptionStore) holds(id []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held[string(id)]
}

// TestR2P6Sec3320Release (decision 243): a resume the adapter installs no
// session for — it re-announces the session the adapter established —
// gives its claim back; a resume it installs leaves the claim to the
// store's owner.
func TestR2P6Sec3320Release(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	respID := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	first, secret1, rid1 := buildResumeSigma1(t)
	second, secret2, rid2 := buildResumeSigma1(t)
	store := &claimingResumptionStore{
		secrets: map[string][]byte{string(rid1): secret1, string(rid2): secret2},
		held:    map[string]bool{},
	}
	// No renewer: a resume on the finished responder keeps its id.
	responder := sigma.NewResponder(respID, caseTestVerifier{}, 0x2001)
	responder.SetResumptionStore(store)
	a := NewCaseAdapter(responder)
	installs := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { installs++; return nil })

	if op, _, err := a.ProcessSigma1(first); err != nil || op != mrp.SCOpcodeSigma2Resume {
		t.Fatalf("first resume: opcode %#x, err %v", op, err)
	}
	if installs != 1 || !store.holds(rid1) {
		t.Fatalf("first resume: %d installs, claim held %v; want 1 and held (the owner ends it)", installs, store.holds(rid1))
	}
	if op, _, err := a.ProcessSigma1(second); err != nil || op != mrp.SCOpcodeSigma2Resume {
		t.Fatalf("second resume: opcode %#x, err %v", op, err)
	}
	if installs != 1 {
		t.Fatalf("second resume installed a session (%d installs); the test needs the re-announced id", installs)
	}
	if store.holds(rid2) {
		t.Fatal("the claim of a resume that installed nothing is still held")
	}
}
