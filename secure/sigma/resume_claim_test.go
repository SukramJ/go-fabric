// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"errors"
	"testing"
)

// claimingStore is a [ResumptionStore] that also claims resumption ids
// ([resumptionClaimer]): an id is held from its claim until its release.
// onClaim, when set, runs once a claim is granted, before the responder
// looks the record up again.
type claimingStore struct {
	*stubResumptionStore
	held     map[string]bool
	claims   int
	releases int
	onClaim  func(id []byte)
}

func newClaimingStore() *claimingStore {
	return &claimingStore{stubResumptionStore: newStubStore(), held: make(map[string]bool)}
}

func (s *claimingStore) ClaimResumptionID(id []byte) (func(), bool) {
	if s.held[string(id)] {
		return nil, false
	}
	s.held[string(id)] = true
	s.claims++
	if s.onClaim != nil {
		s.onClaim(id)
	}
	key := string(id)
	return func() {
		s.releases++
		delete(s.held, key)
	}, true
}

// committingResolver resolves the one destination id it holds, and every
// fabric index, to ours, and counts the commits the responder tells it of
// ([resolutionCommitter]).
type committingResolver struct {
	dest    [32]byte
	ours    *Identity
	commits *int
}

func (c committingResolver) ResolveSigma1Destination(dest [32]byte, _ [RandomSize]byte) (*Identity, PeerVerifier, bool) {
	if dest != c.dest {
		return nil, nil, false
	}
	return c.ours, testVerifier{}, true
}

func (c committingResolver) ResolveFabricIndex(uint8) (*Identity, PeerVerifier, bool) {
	return c.ours, testVerifier{}, true
}

func (c committingResolver) CommitResolution() { *c.commits++ }

// TestResumeClaim_ReleaseGivesTheClaimBackOnce: Release runs the claim's
// release once — every later Release does nothing — and a nil claim or
// one without a release does nothing (R2-P6-sec-3320).
func TestResumeClaim_ReleaseGivesTheClaimBackOnce(t *testing.T) {
	t.Parallel()
	released := 0
	c := &ResumeClaim{release: func() { released++ }}
	c.Release()
	if released != 1 {
		t.Fatalf("the first Release ran the release %d times, want 1", released)
	}
	c.Release()
	c.Release()
	if released != 1 {
		t.Fatalf("Release after the first ran the release again: %d calls, want 1", released)
	}
	var none *ResumeClaim
	none.Release()
	(&ResumeClaim{}).Release()
	if released != 1 {
		t.Fatalf("a nil or empty claim's Release ran a release: %d calls, want 1", released)
	}
}

// TestResumeClaim_HeldIDFallsThroughUntilReleased: a resume through a
// claiming store returns its claim and holds the id; while it is held a
// second Sigma1 presenting the same id is answered by Full Sigma with no
// claim; once the caller releases the claim — it installed no session —
// the id resumes again (R2-P6-sec-3320, decision 243).
func TestResumeClaim_HeldIDFallsThroughUntilReleased(t *testing.T) {
	t.Parallel()
	sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
	store := newClaimingStore()
	store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid})
	respID := newTestIdentity(t, 0xBBBB, 1, fabricIPK())
	resume := func() Sigma1ProcessResult {
		t.Helper()
		responder := NewResponder(respID, testVerifier{}, 0x2001)
		responder.SetResumptionStore(store)
		result, err := responder.ProcessSigma1WithResume(sigma1Bytes)
		if err != nil {
			t.Fatalf("ProcessSigma1WithResume: %v", err)
		}
		return result
	}

	first := resume()
	if !first.IsResume() || first.Claim == nil {
		t.Fatalf("the first resume: resumed %v, claim %v; want a resume with its claim", first.IsResume(), first.Claim)
	}
	if !store.held[string(rid)] || store.claims != 1 {
		t.Fatalf("after the first resume: held %v, claims %d; want the id held by one claim", store.held[string(rid)], store.claims)
	}

	second := resume()
	if second.IsResume() || second.Sigma2 == nil {
		t.Fatal("a Sigma1 presenting a held id resumed; want Full Sigma")
	}
	if second.Claim != nil {
		t.Fatal("the Full Sigma answer to a held id carries a claim")
	}
	if store.claims != 1 || store.releases != 0 {
		t.Fatalf("the refused claim moved the store: claims %d, releases %d; want 1, 0", store.claims, store.releases)
	}

	first.Claim.Release()
	first.Claim.Release()
	if store.held[string(rid)] || store.releases != 1 {
		t.Fatalf("after Release twice: held %v, releases %d; want the id free, one release", store.held[string(rid)], store.releases)
	}

	third := resume()
	if !third.IsResume() || third.Claim == nil {
		t.Fatal("the id did not resume again after its claim was released")
	}
	if third.Claim == first.Claim || store.claims != 2 || !store.held[string(rid)] {
		t.Fatalf("after the resume that followed the release: claims %d, held %v; want a second claim holding the id", store.claims, store.held[string(rid)])
	}
}

// TestResumeClaim_NoClaimWithoutAClaimingStore: a store that claims
// nothing yields a resume without a claim, and a full handshake carries
// none (R2-P6-sec-3320).
func TestResumeClaim_NoClaimWithoutAClaimingStore(t *testing.T) {
	t.Parallel()
	sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
	store := newStubStore()
	store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid})
	respID := newTestIdentity(t, 0xBBBB, 1, fabricIPK())

	responder := NewResponder(respID, testVerifier{}, 0x2001)
	responder.SetResumptionStore(store)
	resumed, err := responder.ProcessSigma1WithResume(sigma1Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.IsResume() || resumed.Claim != nil {
		t.Fatalf("resumed %v, claim %v; want a resume without a claim", resumed.IsResume(), resumed.Claim)
	}

	full := NewResponder(respID, testVerifier{}, 0x2002)
	result, err := full.ProcessSigma1WithResume(sigma1Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsResume() || result.Claim != nil {
		t.Fatalf("resumed %v, claim %v; want a full handshake without a claim", result.IsResume(), result.Claim)
	}
}

// TestResumeClaim_RecordGoneUnderTheClaimIsAMiss: when the record is gone
// once the id is claimed — a resume that held it replaced it and gave the
// id back — the Sigma1 is answered by Full Sigma and the claim is given
// back at once (R2-P6-sec-3320).
func TestResumeClaim_RecordGoneUnderTheClaimIsAMiss(t *testing.T) {
	t.Parallel()
	sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
	store := newClaimingStore()
	store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid})
	store.onClaim = func(id []byte) { delete(store.records, string(id)) }

	responder := NewResponder(newTestIdentity(t, 0xBBBB, 1, fabricIPK()), testVerifier{}, 0x2001)
	responder.SetResumptionStore(store)
	result, err := responder.ProcessSigma1WithResume(sigma1Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsResume() || result.Sigma2 == nil || result.Claim != nil {
		t.Fatalf("resumed %v, claim %v; want Full Sigma without a claim", result.IsResume(), result.Claim)
	}
	if store.claims != 1 || store.releases != 1 || store.held[string(rid)] {
		t.Fatalf("claims %d, releases %d, held %v; want the one claim given back", store.claims, store.releases, store.held[string(rid)])
	}
	if responder.ResumeInfo().Resumed {
		t.Fatal("the responder records a resume for a record gone under the claim")
	}
}

// TestResumeClaim_RefusedForNoSessionIDReleasesAndCommitsNothing: a resume
// refused at its last step — the responder has no session id and the
// renewer gives none — returns the error, gives the claim back and tells
// the resolver nothing; the same resume on a responder with an id commits
// the resolution once (R2-P6-sec-3320, decision 244).
func TestResumeClaim_RefusedForNoSessionIDReleasesAndCommitsNothing(t *testing.T) {
	t.Parallel()
	sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
	store := newClaimingStore()
	store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid})
	ours := newTestIdentity(t, 0xBBBB, 1, fabricIPK())
	commits := 0

	refused := NewResponder(ours, testVerifier{}, 0)
	refused.SetResumptionStore(store)
	refused.SetIdentityResolver(committingResolver{ours: ours, commits: &commits})
	refused.SetSessionIDRenewer(func(uint16) (uint16, bool) { return 0, false })
	if _, err := refused.ProcessSigma1WithResume(sigma1Bytes); !errors.Is(err, errNoSessionID) {
		t.Fatalf("resume without a session id: %v, want %v", err, errNoSessionID)
	}
	if store.claims != 1 || store.releases != 1 || store.held[string(rid)] {
		t.Fatalf("claims %d, releases %d, held %v; want the claim given back", store.claims, store.releases, store.held[string(rid)])
	}
	if commits != 0 {
		t.Fatalf("the refused resume committed the resolution %d times, want 0", commits)
	}
	if _, ok := refused.SessionKeys(); ok || refused.ResumeInfo().Resumed {
		t.Fatal("the refused resume left session keys or a resume record")
	}

	accepted := NewResponder(ours, testVerifier{}, 0x2001)
	accepted.SetResumptionStore(store)
	accepted.SetIdentityResolver(committingResolver{ours: ours, commits: &commits})
	result, err := accepted.ProcessSigma1WithResume(sigma1Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsResume() || result.Claim == nil {
		t.Fatal("the resume on a responder with a session id did not resume with its claim")
	}
	if commits != 1 {
		t.Fatalf("the accepted resume committed the resolution %d times, want 1", commits)
	}
}

// TestResume_RenewerWithoutAnIDKeepsTheOneHeld: a second resume on a
// finished responder asks the renewer for a fresh id; a renewer that has
// none leaves the responder the id it holds rather than refusing.
func TestResume_RenewerWithoutAnIDKeepsTheOneHeld(t *testing.T) {
	t.Parallel()
	sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
	store := newStubStore()
	store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid})
	responder := NewResponder(newTestIdentity(t, 0xBBBB, 1, fabricIPK()), testVerifier{}, 0x2001)
	responder.SetResumptionStore(store)
	asked := 0
	responder.SetSessionIDRenewer(func(uint16) (uint16, bool) {
		asked++
		return 0, false
	})
	for i := range 2 {
		result, err := responder.ProcessSigma1WithResume(sigma1Bytes)
		if err != nil {
			t.Fatalf("resume %d: %v", i+1, err)
		}
		if !result.IsResume() || result.Sigma2Resume.ResponderSessionID != 0x2001 {
			t.Fatalf("resume %d: resumed %v, want a resume announcing 0x2001", i+1, result.IsResume())
		}
	}
	if asked != 1 {
		t.Fatalf("the renewer was asked %d times, want once (the second resume only)", asked)
	}
}

// TestFullSigma_CommitsTheResolutionOnlyOnAcceptance: the resolver learns
// of the Sigma1 its resolution served only once that Sigma1 is accepted —
// not for a Sigma1 refused for its destination, its ephemeral key or the
// missing session id — and exactly once when it is (decision 244).
func TestFullSigma_CommitsTheResolutionOnlyOnAcceptance(t *testing.T) {
	t.Parallel()
	ipk := fabricIPK()
	const fabricID = uint64(0x2A)
	ours := newTestIdentity(t, 0xBBBB, fabricID, ipk)
	initiator := NewInitiator(newTestIdentity(t, 0xAAAA, fabricID, ipk), testVerifier{}, 0x1001, [32]byte{0xAA})
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	resolver := committingResolver{dest: decoded.DestinationID, ours: ours, commits: &commits}

	responder := NewResponder(ours, testVerifier{}, 0x2001)
	responder.SetIdentityResolver(resolver)
	for name, refused := range refusedSigma1s(t, ipk, decoded.DestinationID) {
		if _, err := responder.ProcessSigma1WithResume(refused.payload); !errors.Is(err, refused.want) {
			t.Fatalf("the Sigma1 %s: %v, want %v", name, err, refused.want)
		}
	}
	if commits != 0 {
		t.Fatalf("refused Sigma1s committed the resolution %d times, want 0", commits)
	}

	noID := NewResponder(ours, testVerifier{}, 0)
	noID.SetIdentityResolver(resolver)
	noID.SetSessionIDRenewer(func(uint16) (uint16, bool) { return 0, false })
	if _, err := noID.ProcessSigma1(s1); !errors.Is(err, errNoSessionID) {
		t.Fatalf("a Sigma1 on a responder without a session id: %v, want %v", err, errNoSessionID)
	}
	if commits != 0 {
		t.Fatalf("the Sigma1 refused for its session id committed the resolution %d times, want 0", commits)
	}

	result, err := responder.ProcessSigma1WithResume(s1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sigma2 == nil || result.Claim != nil {
		t.Fatal("the accepted Sigma1 did not answer Sigma2 without a claim")
	}
	if commits != 1 {
		t.Fatalf("the accepted Sigma1 committed the resolution %d times, want 1", commits)
	}
}
