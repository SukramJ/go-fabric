// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// resumeRecords resumes every record it holds; each record's peer is
// another node than the full handshake's initiator.
type resumeRecords map[string][]byte

func (s resumeRecords) GetByID(id []byte) (*sigma.ResumptionRecord, error) {
	secret, ok := s[string(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &sigma.ResumptionRecord{
		SharedSecret: append([]byte(nil), secret...), ResumptionID: append([]byte(nil), id...),
		PeerNodeID: 0xC0FFEE, PeerCATs: []uint32{0x00010001},
	}, nil
}

// TestCaseEstablishedIsTheStepsOwnSession (R5-sec-5101, R5-sec-5102):
// another peer's valid resume Sigma1 is accepted on the same responder
// after the Sigma3 step and before the host has installed the Sigma3's
// session — on its own goroutine in production, from inside the callback
// here, the schedule the read-back of node/security.go left open. The
// session the callback receives is the Sigma3 handshake's: its keys, its
// peer's node id and session id, its session id; the resume's callback
// receives the resume's.
func TestCaseEstablishedIsTheStepsOwnSession(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	initiator := sigma.NewInitiator(newCaseTestIdentity(t, 0xAAAA, 1, ipk), caseTestVerifier{}, 0x1001, [32]byte{0xDE, 0xAD, 0xBE, 0xEF})
	responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
	responder.SetSessionIDRenewer(func(prev uint16) (uint16, bool) { return prev + 1, true })
	resumeSigma1, secret, rid := buildResumeSigma1(t)
	responder.SetResumptionStore(resumeRecords{string(rid): secret})
	a := NewCaseAdapter(responder)

	var got []sigma.Established
	a.SetOnEstablished(func(est sigma.Established) error {
		if len(got) == 0 {
			got = append(got, sigma.Established{}) // the outer call's slot
			if op, _, err := a.ProcessSigma1(resumeSigma1); err != nil || op != mrp.SCOpcodeSigma2Resume {
				t.Fatalf("the interleaved resume: opcode %#x, err %v; want a Sigma2_Resume", op, err)
			}
			got[0] = est
			return nil
		}
		got = append(got, est)
		return nil
	})

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
	_, _, outcome, taken, err := a.processSigma3(s3)
	if err != nil || outcome != sigma3Established {
		t.Fatalf("Sigma3: outcome %d, err %v", outcome, err)
	}
	res, ok := initiator.Result()
	if !ok {
		t.Fatal("the initiator did not finish")
	}
	if len(got) != 2 {
		t.Fatalf("%d sessions handed over, want 2 (the Sigma3's, the resume's)", len(got))
	}
	sigma3, resume := got[0], got[1]
	if sigma3.Keys != res.Keys {
		t.Error("the Sigma3's session does not carry its handshake's keys")
	}
	if sigma3.PeerNodeID != 0xAAAA || sigma3.PeerSessionID != 0x1001 || sigma3.SessionID != res.PeerSessionID || sigma3.SessionID != taken.sessionID {
		t.Errorf("the Sigma3's session: peer node %#x, peer session %#x, session %#x; want 0xaaaa, 0x1001, %#x (the step's %#x)",
			sigma3.PeerNodeID, sigma3.PeerSessionID, sigma3.SessionID, res.PeerSessionID, taken.sessionID)
	}
	if !sigma3.IdentityOK || sigma3.LocalNodeID != 0xBBBB || sigma3.Resume.Resumed || len(sigma3.PeerCATs) != 0 {
		t.Errorf("the Sigma3's session: identity %d/%#x (%v), resumed %v, CATs %v", sigma3.FabricIndex, sigma3.LocalNodeID, sigma3.IdentityOK, sigma3.Resume.Resumed, sigma3.PeerCATs)
	}
	if !bytes.Equal(sigma3.SharedSecret, res.SharedSecret) || !bytes.Equal(sigma3.ResumptionID, res.ResumptionID) {
		t.Error("the Sigma3's session does not carry its handshake's shared secret and resumption id")
	}
	if resume.PeerNodeID != 0xC0FFEE || len(resume.PeerCATs) != 1 || resume.PeerCATs[0] != 0x00010001 || !resume.Resume.Resumed || !bytes.Equal(resume.SharedSecret, secret) {
		t.Errorf("the resume's session: peer node %#x, CATs %v, resumed %v", resume.PeerNodeID, resume.PeerCATs, resume.Resume.Resumed)
	}
	if resume.SessionID == sigma3.SessionID {
		t.Errorf("the resume took the Sigma3's session id %#x", sigma3.SessionID)
	}
}

// TestCaseSessionEstablishedFedFromTheValue: the deprecated callback keeps
// working; its keys and peer session id are the step's own (R5-sec-5102).
func TestCaseSessionEstablishedFedFromTheValue(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	initiator := sigma.NewInitiator(newCaseTestIdentity(t, 0xAAAA, 1, ipk), caseTestVerifier{}, 0x1001, [32]byte{0xDE, 0xAD, 0xBE, 0xEF})
	a := NewCaseAdapter(sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001))
	type call struct {
		keys sigma.SessionKeys
		peer uint16
	}
	var calls []call
	a.SetOnSessionEstablished(func(keys sigma.SessionKeys, peerSessionID uint16) error {
		calls = append(calls, call{keys, peerSessionID})
		return nil
	})
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
	if _, _, err := a.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	res, _ := initiator.Result()
	if len(calls) != 1 || calls[0].keys != res.Keys || calls[0].peer != 0x1001 {
		t.Fatalf("calls %d (peer session %v); want one with the handshake's keys and 0x1001", len(calls), calls)
	}
	// A callback of the new setter takes precedence.
	var value int
	a.SetOnEstablished(func(sigma.Established) error { value++; return nil })
	a.SetResponder(sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2002))
	initiator = sigma.NewInitiator(newCaseTestIdentity(t, 0xAAAA, 1, ipk), caseTestVerifier{}, 0x1003, [32]byte{0x01})
	if s1, err = initiator.GenerateSigma1(); err != nil {
		t.Fatal(err)
	}
	if _, s2, err = a.ProcessSigma1(s1); err != nil {
		t.Fatal(err)
	}
	if s3, err = initiator.ProcessSigma2Bytes(s2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || value != 1 {
		t.Fatalf("deprecated callback ran %d times, the new one %d; want 1 and 1", len(calls), value)
	}
}
