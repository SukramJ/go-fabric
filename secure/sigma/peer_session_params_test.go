// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma

import "testing"

// TestPeerSessionParametersFollowSigma1 pins the accessor session-open
// callers copy onto the operational entry: the initiator's MRP hints
// (Sigma1 tag 5, matter.js Sigma1 `initiatorSessionParams`) are reported
// with ok=true only when the initiator supplied them, and read back
// exactly, at their uint32 width.
func TestPeerSessionParametersFollowSigma1(t *testing.T) {
	t.Parallel()
	ipk := fabricIPK()
	id := newTestIdentity(t, 0xCCCC, 5, ipk)
	verifier := testVerifier{}

	resp := NewResponder(id, verifier, 0x3001)
	if _, ok := resp.PeerSessionParameters(); ok {
		t.Fatal("a responder that saw no Sigma1 reported peer session parameters")
	}

	init := NewInitiator(id, verifier, 0x4001, [RandomSize]byte{7, 7, 7})
	sigma1Bytes, err := init.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := UnmarshalSigma1(sigma1Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sigma1.InitiatorSessionParams = &SessionParameters{
		SessionIdleInterval:      100000,
		SessionActiveInterval:    70000,
		SessionActiveThreshold:   4000,
		DataModelRevision:        21,
		InteractionModelRevision: 12,
		SpecificationVersion:     0x01040000,
	}
	if _, err := resp.ProcessSigma1(sigma1.Marshal()); err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	got, ok := resp.PeerSessionParameters()
	if !ok {
		t.Fatal("initiator supplied session parameters; PeerSessionParameters reported none")
	}
	if got != *sigma1.InitiatorSessionParams {
		t.Fatalf("PeerSessionParameters = %+v, want %+v", got, *sigma1.InitiatorSessionParams)
	}
}
