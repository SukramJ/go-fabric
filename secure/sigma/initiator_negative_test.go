// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
)

// TestInitiatorRejectsSigma2WithForeignTranscriptSignature pins the
// initiator's half of the CASE proof of possession (initiator.go
// ProcessSigma2, the verifyTranscript call on the TBS2 signature). The
// responder presents the right NOC — node id, fabric id and chain all
// check out — but signs the transcript with a key that NOC does not
// carry: a node of the fabric replaying another node's certificate,
// which is public data. Only the transcript signature tells the two
// apart, so without it a controller would bind a session to a peer that
// never proved it holds the key (R2-P5-tmut-03). Both initiator shapes
// are covered: the peer initiator a controller uses and the plain one.
func TestInitiatorRejectsSigma2WithForeignTranscriptSignature(t *testing.T) {
	t.Parallel()
	impostorKey := func(t *testing.T, id *Identity) *Identity {
		t.Helper()
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		forged := *id
		forged.PrivateKey = k // the NOC still names id's key
		return &forged
	}

	t.Run("peer initiator", func(t *testing.T) {
		t.Parallel()
		f := newPeerFixture(t)
		initiator := f.initiator(t, 0x1111, nil)
		responder := NewResponder(impostorKey(t, f.controller), f.verifier, 0x2222)
		s1, err := initiator.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		sigma2, err := responder.ProcessSigma1(s1)
		if err != nil {
			t.Fatal(err)
		}
		s3, err := initiator.ProcessSigma2Bytes(sigma2.Marshal())
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("ProcessSigma2Bytes err = %v, want ErrSignatureInvalid", err)
		}
		if s3 != nil {
			t.Errorf("a Sigma3 (%d bytes) was produced for an unauthenticated responder", len(s3))
		}
		if _, ok := initiator.SessionKeys(); ok {
			t.Error("SessionKeys available after a refused Sigma2")
		}
		if _, ok := initiator.Result(); ok {
			t.Error("Result available after a refused Sigma2")
		}
	})

	t.Run("plain initiator", func(t *testing.T) {
		t.Parallel()
		ipk := fabricIPK()
		initID := newTestIdentity(t, 0xAAAA, 1, ipk)
		respID := newTestIdentity(t, 0xBBBB, 1, ipk)
		initiator := NewInitiator(initID, testVerifier{}, 0x1001, [32]byte{0x01})
		responder := NewResponder(impostorKey(t, respID), testVerifier{}, 0x2001)
		s1, err := initiator.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		sigma2, err := responder.ProcessSigma1(s1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := initiator.ProcessSigma2(sigma2); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("ProcessSigma2 err = %v, want ErrSignatureInvalid", err)
		}
		if _, ok := initiator.SessionKeys(); ok {
			t.Error("SessionKeys available after a refused Sigma2")
		}
	})
}

// TestInitiatorSigma2ResumeOutOfOrder pins ProcessSigma2Resume's state
// gate (initiator.go: state != initiatorStateSigma1Sent →
// ErrSessionState). A Sigma2_Resume is an answer to this initiator's
// Sigma1 and to nothing else: one that arrives before Sigma1, or a
// second one after the session was resumed, must not run the key
// schedule again (R2-P5-tmut-10). "after completion" replays a genuine
// Sigma2_Resume from the in-module responder, which would verify a second
// time, so only the state gate refuses it; "before Sigma1" sends a
// zero-filled one, which without the gate would fail its MIC check
// (ErrUnauthenticated), not the state check asserted here.
func TestInitiatorSigma2ResumeOutOfOrder(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)

	// A full handshake yields the resumption record both sides hold.
	first := f.initiator(t, 0x1111, nil)
	resp1 := f.responder(0x2222)
	s1, err := first.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma2, err := resp1.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := first.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := resp1.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	firstRes, _ := first.Result()
	store := mapResumptionStore{string(resp1.ResumptionID()): {
		SharedSecret: resp1.ECDHSharedSecret(),
		ResumptionID: resp1.ResumptionID(),
		FabricIndex:  1,
		PeerNodeID:   f.device.NodeID,
	}}
	record := &ResumptionRecord{
		SharedSecret: firstRes.SharedSecret,
		ResumptionID: firstRes.ResumptionID,
		PeerNodeID:   f.controller.NodeID,
	}

	t.Run("before Sigma1", func(t *testing.T) {
		t.Parallel()
		fresh := f.initiator(t, 0x1113, record)
		raw := MarshalSigma2Resume(Sigma2Resume{ResumptionID: make([]byte, ResumptionIDSize), Sigma2ResumeMIC: make([]byte, ResumptionIDSize), ResponderSessionID: 9})
		if err := fresh.ProcessSigma2Resume(raw); !errors.Is(err, ErrSessionState) {
			t.Fatalf("ProcessSigma2Resume before Sigma1: err = %v, want ErrSessionState", err)
		}
		if _, ok := fresh.SessionKeys(); ok {
			t.Error("SessionKeys available after a Sigma2_Resume before Sigma1")
		}
	})

	t.Run("after completion", func(t *testing.T) {
		t.Parallel()
		second := f.initiator(t, 0x1112, record)
		resp2 := f.responder(0x2223)
		resp2.SetResumptionStore(store)
		s1b, err := second.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		result, err := resp2.ProcessSigma1WithResume(s1b)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsResume() {
			t.Fatal("responder did not take the resumption fast path")
		}
		raw := MarshalSigma2Resume(*result.Sigma2Resume)
		if err := second.ProcessSigma2Resume(raw); err != nil {
			t.Fatalf("first ProcessSigma2Resume: %v", err)
		}
		before, ok := second.SessionKeys()
		if !ok {
			t.Fatal("no session keys after a resumption")
		}
		if err := second.ProcessSigma2Resume(raw); !errors.Is(err, ErrSessionState) {
			t.Fatalf("second ProcessSigma2Resume: err = %v, want ErrSessionState", err)
		}
		after, ok := second.SessionKeys()
		if !ok || !constantTimeKeysEqual(before, after) {
			t.Error("a refused second Sigma2_Resume changed the session keys")
		}
	})
}
