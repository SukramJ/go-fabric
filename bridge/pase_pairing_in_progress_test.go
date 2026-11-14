// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// While a commissioner's PASE handshake is in progress, a PASE message of
// any other handshake — another exchange, or another initiator on the same
// exchange id — is ignored before a handler is resolved: it neither counts
// toward the brute-force cap nor ends the pairing. matter.js ignores every
// new exchange while a pairing is in progress
// (packages/protocol/src/session/pase/PaseServer.ts:80-86 at db021da0) and
// counts only errors of the pairing it accepted (:94-95); review II,
// R2-P3-lan-dev-03.
func TestPASEMessagesOfOtherHandshakesLeaveAPairingInProgressAlone(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")
	var paseSessions, adapters atomic.Int32
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		adapters.Add(1)
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

	// Pake3s of other handshakes: each on an exchange of its own, and one
	// of another initiator on the commissioner's exchange id.
	for i := range paseMaxErrors {
		_ = other.send(mrp.SCOpcodePake3, uint16(0x9000+i), []byte{0x15}, nil)
	}
	_ = other.send(mrp.SCOpcodePake1, paseExchange, []byte{0x15}, nil)
	if got := r.b.paseFailures.Load(); got != 0 {
		t.Fatalf("PASE failures counted for other handshakes' messages during a pairing: %d, want 0", got)
	}
	if r.b.paseLockedOut() {
		t.Fatal("PASE locked out by other handshakes' messages while a commissioner's pairing was in progress")
	}
	if got := adapters.Load(); got != 1 {
		t.Fatalf("PASE handlers resolved: %d, want 1 (the commissioner's)", got)
	}

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
	_ = commissioner.send(mrp.SCOpcodePake1, paseExchange, spake2.EncodePake1(pA), &counter)
	pake2, counter, _ := commissioner.recv(mrp.SCOpcodePake2, "Pake2")
	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		t.Fatal(err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		t.Fatal(err)
	}
	_ = commissioner.send(mrp.SCOpcodePake3, paseExchange, spake2.EncodePake3(cA), &counter)
	if got := paseSessions.Load(); got != 1 {
		t.Fatalf("PASE sessions established: %d, want 1", got)
	}
}
