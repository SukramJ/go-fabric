// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestSigma3OfACompletedHandshakeIsAnsweredSuccess: a Sigma3 that reaches
// the router again after its handshake completed — a retransmission the
// unsecured duplicate window does not see, as for a source past
// maxUnsecuredWindows — meets the completed handshake's adapter, whose
// answer is the success report again, and installs no second session; it
// is not taken for a Sigma3 of a handshake no Sigma1 began (review II,
// R2-P3-dr-dev-02).
func TestSigma3OfACompletedHandshakeIsAnsweredSuccess(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	var sessions, responders atomic.Int32
	provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
		responder := sigma.NewResponder(ours, caseTestVerifier{}, uint16(0x2000+responders.Add(1)))
		responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { sessions.Add(1); return nil })
		return a
	})
	provider.SetOnEvict(r.b.ForgetSigma1Replied)
	r.b.AttachCaseHandlerProvider(provider.Resolve)

	controller := newPipelinePeer(t, r.b, 0xC0DE)
	const caseExchange = uint16(0x0202)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, ipk), Verifier: caseTestVerifier{},
		SessionID: 0x1002, PeerNodeID: 0xBBBB, RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.send(mrp.SCOpcodeSigma1, caseExchange, sigma1, nil); err != nil {
		t.Fatal(err)
	}
	sigma2, counter, _ := controller.recv(mrp.SCOpcodeSigma2, "Sigma2")
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.send(mrp.SCOpcodeSigma3, caseExchange, sigma3, &counter); err != nil {
		t.Fatal(err)
	}
	report, _, _ := controller.recv(mrp.SCOpcodeStatusReport, "the report on Sigma3")
	if general, _, _, ok := decodeStatusReport(report); !ok || general != mrp.SCStatusGeneralSuccess {
		t.Fatalf("the report on Sigma3: general status %d, want SUCCESS", general)
	}

	if err := controller.send(mrp.SCOpcodeSigma3, caseExchange, sigma3, &counter); err != nil {
		t.Fatal(err)
	}
	again, _, _ := controller.recv(mrp.SCOpcodeStatusReport, "the report on the Sigma3 that arrived again")
	if general, _, protocol, ok := decodeStatusReport(again); !ok || general != mrp.SCStatusGeneralSuccess {
		t.Fatalf("the Sigma3 of the completed handshake, arriving again, is answered general=%d protocol=%d, want the success report", general, protocol)
	}
	if got := sessions.Load(); got != 1 {
		t.Fatalf("the handshake installed %d sessions, want 1", got)
	}
}
