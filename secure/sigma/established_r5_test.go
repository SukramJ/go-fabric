// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"testing"
)

// TestProcessSigma3EstablishedReturnsTheStepsSession (R5-sec-5101): the
// Sigma3 that finishes the handshake returns its session as a value of its
// own — the keys, ids, peer and record material the accessors report at
// that step, copies a caller may keep — and the accepted retransmission of
// that Sigma3 returns none.
func TestProcessSigma3EstablishedReturnsTheStepsSession(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	initiator := f.initiator(t, 0x1111, nil)
	responder := f.responder(0x2222)
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	est, err := responder.ProcessSigma3Established(s3)
	if err != nil || est == nil {
		t.Fatalf("ProcessSigma3Established: %v, %v", est, err)
	}
	keys, _ := responder.SessionKeys()
	fi, nid, ok := responder.SessionIdentity()
	params, paramsOK := responder.PeerSessionParameters()
	if est.Keys != keys || est.SessionID != responder.SessionID() || est.PeerSessionID != 0x1111 ||
		est.PeerNodeID != responder.PeerNodeID() || est.FabricIndex != fi || est.LocalNodeID != nid || est.IdentityOK != ok ||
		est.PeerSessionParams != params || est.PeerSessionParamsOK != paramsOK || est.Resume.Resumed {
		t.Fatalf("the value %+v is not the step's session", est)
	}
	if !bytes.Equal(est.SharedSecret, responder.ECDHSharedSecret()) || !bytes.Equal(est.ResumptionID, responder.ResumptionID()) {
		t.Fatal("the value's record material is not the step's")
	}
	est.SharedSecret[0] ^= 0xFF
	est.ResumptionID[0] ^= 0xFF
	if bytes.Equal(est.SharedSecret, responder.ECDHSharedSecret()) || bytes.Equal(est.ResumptionID, responder.ResumptionID()) {
		t.Fatal("the value shares its slices with the responder")
	}
	again, err := responder.ProcessSigma3Established(s3)
	if err != nil || again != nil {
		t.Fatalf("the finishing Sigma3 again: %v, %v; want no value and no error", again, err)
	}
}
