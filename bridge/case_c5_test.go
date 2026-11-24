// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestCaseStepTakesTheStatedSessionParams (R5-test-5105, the pacing half
// of R4-sec-4401): the Sigma1 step takes the SessionParameters its own
// Sigma1 states, and the Sigma3 step the ones of the handshake it
// completes — the values the bridge paces each reply by.
func TestCaseStepTakesTheStatedSessionParams(t *testing.T) {
	t.Parallel()
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the tests' encoding of the root key
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	responder := sigma.NewResponder(ours, caseTestVerifier{}, 0x2001)
	responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
	a := NewCaseAdapter(responder)
	a.SetOnEstablished(func(sigma.Established) error { return nil })

	stated := sessionparams.Parameters{SessionIdleInterval: 4321, SessionActiveInterval: 321, SessionActiveThreshold: 4000}
	ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1002, PeerNodeID: 0xBBBB, RootPublicKey: rootPub, SessionParams: &stated,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := ini.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	op, s2, taken1, err := a.processSigma1(s1)
	if err != nil || op != mrp.SCOpcodeSigma2 {
		t.Fatalf("Sigma1: opcode %#x, err %v; want a Sigma2", op, err)
	}
	if taken1.peer == nil || *taken1.peer != stated {
		t.Fatalf("the Sigma1 step took %+v; want the parameters its Sigma1 states, %+v", taken1.peer, stated)
	}
	s3, err := ini.ProcessSigma2Bytes(s2)
	if err != nil {
		t.Fatal(err)
	}
	_, _, outcome, taken3, err := a.processSigma3(s3)
	if err != nil || outcome != sigma3Established {
		t.Fatalf("Sigma3: outcome %d, err %v; want established", outcome, err)
	}
	if taken3.peer == nil || *taken3.peer != stated {
		t.Fatalf("the Sigma3 step took %+v; want the parameters of the handshake it completes, %+v", taken3.peer, stated)
	}
}

// TestUninstalledResumeClearsItsSecret (C5-h1-n3-secret-copy = G641-1,
// decision 658): a resume that installs no session — the replay of one
// that did, or one on an adapter without an established-callback — drops
// the step's session value with its shared secret zeroed, as an installed
// one hands it to the record (BD-chip-HandshakeSecretsOverwritten).
func TestUninstalledResumeClearsItsSecret(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		callback bool
		dropped  int // the call whose value is dropped, 1-based
	}{
		{"replayed resume", true, 2},
		{"no established-callback", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ipk := newCaseTestIPK(t)
			responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
			s1, secret, rid := buildResumeSigma1(t)
			responder.SetResumptionStore(resumeRecords{string(rid): secret})
			a := NewCaseAdapter(responder)
			if tc.callback {
				a.SetOnEstablished(func(sigma.Established) error { return nil })
			}
			var dropped []sigma.Established
			a.resumeDroppedForTest = func(est *sigma.Established) { dropped = append(dropped, *est) }
			for call := 1; call <= tc.dropped; call++ {
				if op, _, err := a.ProcessSigma1(s1); err != nil || op != mrp.SCOpcodeSigma2Resume {
					t.Fatalf("resume %d: opcode %#x, err %v; want a Sigma2_Resume", call, op, err)
				}
				if call < tc.dropped && len(dropped) != 0 {
					t.Fatalf("resume %d installed its session and dropped %d values", call, len(dropped))
				}
			}
			if len(dropped) != 1 {
				t.Fatalf("%d values dropped; want 1", len(dropped))
			}
			got := dropped[0].SharedSecret
			if len(got) == 0 {
				t.Fatal("the dropped value carries no shared secret")
			}
			for _, b := range got {
				if b != 0 {
					t.Fatalf("the dropped value's shared secret is %x; want it zeroed", got)
				}
			}
		})
	}
}
