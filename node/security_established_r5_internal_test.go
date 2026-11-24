// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestCASEResponderInstallsTheSigma3SessionDespiteAResumeMeanwhile
// (R5-sec-5101): a resume Sigma1 is accepted on the adapter whose Sigma3
// step established a session, while the node installs that session — on
// its own goroutine in production, here from the install's test seam. The
// Sigma3 installs its own session — its peer session id, and a record of
// its own shared secret and resumption id — or, its stake renewed by the
// resume, nothing: never the resumed record's secret under the Sigma3
// peer's record. A guard of the node's install from the handshake's value,
// not the row's red test (that is bridge's
// TestCaseEstablishedIsTheStepsOwnSession): the stake the resume renews
// refuses the install at this schedule at 25a78a453 too.
func TestCASEResponderInstallsTheSigma3SessionDespiteAResumeMeanwhile(t *testing.T) {
	t.Parallel()
	r := newRecordRig(t)
	rec1 := r.established(t)

	a, est := r.adapter(t)
	resumer, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: r.fx.peer, Verifier: r.fx.verifier, SessionID: 0x1002,
		PeerNodeID: r.fx.local.NodeID, RootPublicKey: r.fx.root,
		Resumption: &sigma.ResumptionRecord{
			SharedSecret: rec1.SharedSecret, ResumptionID: rec1.ResumptionID,
			FabricIndex: rec1.FabricIndex, PeerNodeID: r.fx.local.NodeID, PeerCATs: rec1.CASEAuthTags,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resumeSigma1, err := resumer.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	interleaved := false
	var resumeOp uint8
	r.fx.ids.testAfterStake = func() {
		if interleaved {
			return
		}
		interleaved = true
		var err error
		if resumeOp, _, err = a.ProcessSigma1(resumeSigma1); err != nil {
			t.Errorf("the interleaved resume: %v", err)
		}
	}

	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: r.fx.peer, Verifier: r.fx.verifier, SessionID: 0x1001,
		PeerNodeID: r.fx.local.NodeID, RootPublicKey: r.fx.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := a.ProcessSigma1(s1)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	s3, err := initiator.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatalf("ProcessSigma2: %v", err)
	}
	_, _, pickup := a.ProcessSigma3(s3)
	if resumeOp != mrp.SCOpcodeSigma2Resume {
		t.Fatalf("the interleaved resume answered %#x; the test checks nothing", resumeOp)
	}
	res, ok := initiator.Result()
	if !ok {
		t.Fatal("the initiator did not finish")
	}
	if pickup != nil {
		// The resume renewed the handshake's stake: nothing is installed
		// for the Sigma3, and no record is saved for it.
		if !errors.Is(pickup, operational.ErrStakeLost) {
			t.Fatalf("ProcessSigma3: %v; want the Sigma3's session installed or its stake lost", pickup)
		}
		if _, err := r.sessions.Get(res.PeerSessionID); err == nil {
			t.Fatalf("a session %#x is installed for the Sigma3 whose stake was lost", res.PeerSessionID)
		}
		if rec, ok := r.record(t); !ok || !bytes.Equal(rec.SharedSecret, rec1.SharedSecret) {
			t.Fatal("the peer's record moved although the Sigma3 installed nothing")
		}
		return
	}
	entry, err := r.sessions.Get(res.PeerSessionID)
	if err != nil {
		t.Fatalf("the Sigma3's session %#x is not installed: %v", res.PeerSessionID, err)
	}
	if entry.Session.PeerSessionID() != 0x1001 {
		t.Errorf("the Sigma3's session stamps peer session %#x, want 0x1001", entry.Session.PeerSessionID())
	}
	if !est.complete(res.PeerSessionID, true) {
		t.Fatal("the Sigma3's session is not established")
	}
	rec, ok := r.record(t)
	if !ok {
		t.Fatal("no record saved for the Sigma3's session")
	}
	if bytes.Equal(rec.SharedSecret, rec1.SharedSecret) {
		t.Fatal("the record saved for the Sigma3's session carries the resumed record's shared secret")
	}
	if !bytes.Equal(rec.SharedSecret, res.SharedSecret) || !bytes.Equal(rec.ResumptionID, res.ResumptionID) {
		t.Errorf("the record saved for the Sigma3's session is not its handshake's (resumption id %x, want %x)", rec.ResumptionID, res.ResumptionID)
	}
}
