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

// TestResumeAwaitsItsOwnInstalledSession (R5-sec-5103): two resumes on one
// adapter, the second accepted inside the first's established-callback and
// settled by the router between the first's step and the first's
// settlement — another goroutine's schedule, reproduced in line. Each
// awaits the initiator's Success for the session its own step installed;
// the first, settled last, holds the adapter's one wait with its own id.
func TestResumeAwaitsItsOwnInstalledSession(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	ipk := newCaseTestIPK(t)
	responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
	responder.SetSessionIDRenewer(func(prev uint16) (uint16, bool) { return prev + 1, true })
	first, secret1, rid1 := buildResumeSigma1(t)
	second, secret2, rid2 := buildResumeSigma1(t)
	responder.SetResumptionStore(resumeRecords{string(rid1): secret1, string(rid2): secret2})
	a := NewCaseAdapter(responder)

	hdr2, proto2 := r.request(mrp.SCOpcodeSigma1, 0x7002)
	step2 := &sigma1Step{handler: a}
	var op2 uint8
	var out2 []byte
	var err2 error
	calls := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error {
		calls++
		if calls == 1 {
			op2, out2, err2 = processSigma1With(a, second, step2)
		}
		return nil
	})

	hdr1, proto1 := r.request(mrp.SCOpcodeSigma1, 0x7001)
	step1 := &sigma1Step{handler: a}
	process1 := func(p []byte) (uint8, []byte, error) {
		op, out, err := processSigma1With(a, p, step1)
		// The second resume's goroutine settles before this one does.
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr2, proto2, nil, "sigma1",
			func([]byte) (uint8, []byte, error) { return op2, out2, err2 }, step2); err != nil {
			t.Errorf("handleCase (second): %v", err)
		}
		return op, out, err
	}
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr1, proto1, first, "sigma1", process1, step1); err != nil {
		t.Fatalf("handleCase (first): %v", err)
	}
	if op2 != mrp.SCOpcodeSigma2Resume || err2 != nil {
		t.Fatalf("the second resume: opcode %#x, err %v", op2, err2)
	}
	id1, ok1 := step1.establishedSession()
	id2, ok2 := step2.establishedSession()
	if !ok1 || !ok2 || id1 == id2 {
		t.Fatalf("installed %#x (%v), %#x (%v); want two sessions", id1, ok1, id2, ok2)
	}
	if got, ok := a.takeAwaitedResume(handshakeKeyOf(hdr1, proto1.ExchangeID)); !ok || got != id1 {
		t.Fatalf("the first resume awaits %#x (%v); want its own session %#x (the second's is %#x)", got, ok, id1, id2)
	}
}

// TestSigma3WithoutStepValueIsNoEstablishment (R5-sec-5101, follow-up N1
// of its Fable diff read): a responder handed over already finished accepts the
// finishing Sigma3's retransmission and builds no session value. With no
// established-callback installed the step must not report an established
// session (it named session 0 before) — it fails, and the adapter does
// not read established afterwards.
func TestSigma3WithoutStepValueIsNoEstablishment(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	initiator := sigma.NewInitiator(newCaseTestIdentity(t, 0xAAAA, 1, ipk), caseTestVerifier{}, 0x1001, [32]byte{0xDE, 0xAD, 0xBE, 0xEF})
	responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := NewCaseAdapter(responder).ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}
	if est, err := responder.ProcessSigma3Established(s3); err != nil || est == nil {
		t.Fatalf("the finishing Sigma3: est %v, err %v", est, err)
	}

	a := NewCaseAdapter(nil)
	a.SetResponder(responder) // already finished; no callback installed
	op, _, outcome, taken, err := a.processSigma3(s3)
	if err == nil || outcome != sigma3Failed || op != 0 || taken.named {
		t.Fatalf("retransmitted Sigma3 without a step value: opcode %#x, outcome %d, named %v (session %#x), err %v; want a failed step",
			op, outcome, taken.named, taken.sessionID, err)
	}
	a.mu.RLock()
	established, id := a.established, a.establishedSessionID
	a.mu.RUnlock()
	if established || id != 0 {
		t.Fatalf("adapter reads established %v (session %#x) after a step without a value", established, id)
	}
}
