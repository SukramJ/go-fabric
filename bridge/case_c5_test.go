// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
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
			a.droppedForTest = func(est *sigma.Established) { dropped = append(dropped, *est) }
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

// zeroedA7 fails unless every dropped value carries a shared secret, all
// of it zero.
func zeroedA7(t *testing.T, dropped []sigma.Established) {
	t.Helper()
	if len(dropped) == 0 {
		t.Fatal("no session value dropped")
	}
	for i := range dropped {
		est := &dropped[i]
		if len(est.SharedSecret) == 0 {
			t.Fatalf("dropped value %d carries no shared secret", i)
		}
		for _, b := range est.SharedSecret {
			if b != 0 {
				t.Fatalf("dropped value %d's shared secret is %x; want it zeroed", i, est.SharedSecret)
			}
		}
	}
}

// sigma3AdapterA7 runs a full handshake up to its Sigma3 on a fresh
// adapter and returns the adapter, the Sigma3 and the values it drops.
func sigma3AdapterA7(t *testing.T) (*CaseAdapter, []byte, *[]sigma.Established) {
	t.Helper()
	ipk := newCaseTestIPK(t)
	initiator := sigma.NewInitiator(newCaseTestIdentity(t, 0xAAAA, 1, ipk), caseTestVerifier{}, 0x1001, [32]byte{0xDE, 0xAD, 0xBE, 0xEF})
	a := NewCaseAdapter(sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001))
	dropped := &[]sigma.Established{}
	a.droppedForTest = func(est *sigma.Established) { *dropped = append(*dropped, *est) }
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
	return a, s3, dropped
}

// TestDroppedSessionValuesClearTheirSecret (N1 of the Fable diff read of
// C5-h1-n3): every step's session value this module drops — a resume or a
// Sigma3 whose callback refused it, a Sigma3 without a callback, a session
// installed through the deprecated callback, which is handed the keys
// alone — is dropped with its shared secret zeroed.
func TestDroppedSessionValuesClearTheirSecret(t *testing.T) {
	t.Parallel()
	refuse := func(sigma.Established) error { return errors.New("refused") }
	t.Run("resume refused by the callback", func(t *testing.T) {
		t.Parallel()
		ipk := newCaseTestIPK(t)
		responder := sigma.NewResponder(newCaseTestIdentity(t, 0xBBBB, 1, ipk), caseTestVerifier{}, 0x2001)
		s1, secret, rid := buildResumeSigma1(t)
		responder.SetResumptionStore(resumeRecords{string(rid): secret})
		a := NewCaseAdapter(responder)
		a.SetOnEstablished(refuse)
		var dropped []sigma.Established
		a.droppedForTest = func(est *sigma.Established) { dropped = append(dropped, *est) }
		if _, _, err := a.ProcessSigma1(s1); err == nil {
			t.Fatal("a resume the callback refused: want an error")
		}
		zeroedA7(t, dropped)
	})
	t.Run("Sigma3 refused by the callback", func(t *testing.T) {
		t.Parallel()
		a, s3, dropped := sigma3AdapterA7(t)
		a.SetOnEstablished(refuse)
		if _, _, err := a.ProcessSigma3(s3); err == nil {
			t.Fatal("a Sigma3 the callback refused: want an error")
		}
		zeroedA7(t, *dropped)
	})
	t.Run("Sigma3 without a callback", func(t *testing.T) {
		t.Parallel()
		a, s3, dropped := sigma3AdapterA7(t)
		if _, _, err := a.ProcessSigma3(s3); err != nil {
			t.Fatal(err)
		}
		zeroedA7(t, *dropped)
	})
	t.Run("Sigma3 through the deprecated callback", func(t *testing.T) {
		t.Parallel()
		a, s3, dropped := sigma3AdapterA7(t)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { return nil })
		if _, _, err := a.ProcessSigma3(s3); err != nil {
			t.Fatal(err)
		}
		zeroedA7(t, *dropped)
	})
}
