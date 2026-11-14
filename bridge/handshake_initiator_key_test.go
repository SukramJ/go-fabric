// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// A handshake's state belongs to the initiator that began it: matter.js finds
// the unsecured session by the initiator's node id and the exchange inside it
// (packages/protocol/src/protocol/ExchangeManager.ts:308-312 and :401 at
// db021da0). A message from another source node id that names the same
// exchange id meets another handshake, and the first initiator's next message
// still completes its own (review II, R2-P3-lan-dev-01).

func TestCASEHandshakeIsNotReachedByAnotherInitiatorOnItsExchange(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)

	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // SA1019: test fixture
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	var caseSessions, responders atomic.Int32
	provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
		responder := sigma.NewResponder(ours, caseTestVerifier{}, uint16(0x2000+responders.Add(1)))
		responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { caseSessions.Add(1); return nil })
		return a
	})
	r.b.AttachCaseHandlerProvider(provider.Resolve)

	controller := newPipelinePeer(t, r.b, 0xC0DE)
	other := newPipelinePeer(t, r.b, 0xBAD0BAD0) // its own address, its own source node id
	const caseExchange = uint16(0x0202)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity:      newCaseTestIdentity(t, 0xAAAA, 1, ipk),
		Verifier:      caseTestVerifier{},
		SessionID:     0x1002,
		PeerNodeID:    0xBBBB,
		RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.send(mrp.SCOpcodeSigma1, caseExchange, sigma1, nil); err != nil {
		t.Fatalf("Sigma1: %v", err)
	}
	sigma2, counter, _ := controller.recv(mrp.SCOpcodeSigma2, "Sigma2")

	// Another initiator's Sigma3 on the same exchange id, between the
	// controller's Sigma2 and its Sigma3.
	_ = other.send(mrp.SCOpcodeSigma3, caseExchange, []byte{0x15, 0x18}, nil)
	if !r.b.outboundReliable.holdsUnsecuredExchange(HandshakeKey{InitiatorNodeID: controller.nodeID, ExchangeID: caseExchange}) {
		t.Fatal("the controller's Sigma2 stopped waiting for its acknowledgement when another initiator's Sigma3 named its exchange id")
	}

	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("the controller refused Sigma2: %v", err)
	}
	_ = controller.send(mrp.SCOpcodeSigma3, caseExchange, sigma3, &counter)
	if got := caseSessions.Load(); got != 1 {
		t.Fatalf("CASE sessions established for the controller: %d, want 1 — another initiator's Sigma3 on the "+
			"controller's exchange id reached the controller's responder", got)
	}
}

func TestPASEHandshakeIsNotReachedByAnotherInitiatorOnItsExchange(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")
	var paseSessions atomic.Int32
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(iterations, salt, 1)
		a.SetOnSessionEstablished(func([]byte, uint16) error { paseSessions.Add(1); return nil })
		return a
	})
	r.b.AttachPaseHandlerProvider(provider.Resolve)

	commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
	other := newPipelinePeer(t, r.b, 0xBAD0BAD0)
	const paseExchange = uint16(0x0101)
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	pbkdfReq := buildTestPBKDFParamRequest(t, initRand)
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, paseExchange, pbkdfReq, nil); err != nil {
		t.Fatal(err)
	}
	pbkdfResp, counter, _ := commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "PBKDFParamResponse")
	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(pbkdfReq)
	h.Write(pbkdfResp)
	prover, err := spake2.NewProver(passcode, salt, iterations, nil, nil, h.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	if err := commissioner.send(mrp.SCOpcodePake1, paseExchange, spake2.EncodePake1(pA), &counter); err != nil {
		t.Fatal(err)
	}
	pake2, counter, _ := commissioner.recv(mrp.SCOpcodePake2, "Pake2")

	// Another initiator's Pake3 on the same exchange id, between the
	// commissioner's Pake2 and its Pake3.
	_ = other.send(mrp.SCOpcodePake3, paseExchange, []byte{0x15, 0x30, 0x01, 0x20}, nil)

	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		t.Fatal(err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		t.Fatalf("the commissioner refused Pake2: %v", err)
	}
	_ = commissioner.send(mrp.SCOpcodePake3, paseExchange, spake2.EncodePake3(cA), &counter)
	if got := paseSessions.Load(); got != 1 {
		t.Fatalf("PASE sessions established for the commissioner: %d, want 1 — another initiator's Pake3 on the "+
			"commissioner's exchange id reached the commissioner's handshake", got)
	}
}

// onExchange is the key of a handshake on exchangeID whose initiator named
// no node id — the key every test that predates [HandshakeKey] used.
func onExchange(exchangeID uint16) HandshakeKey { return HandshakeKey{ExchangeID: exchangeID} }
