// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestPaseRefusalEndsTheTranscript: a refused PASE step ends the
// handshake — the router sends the failure report, counts it and frees
// the single-active-PASE claim — so the PBKDFParamRequest/Response
// transcript the adapter captured for it goes with it. A Pake1 that
// follows on the same exchange, without a new PBKDFParamRequest, reaches
// no verifier and is answered with no Pake2. With a handler provider the
// router already drops that Pake1 (the handshake no longer holds the
// claim, resolvePaseContinuation); the singleton handler of
// AttachPaseHandler is handed every Pake1, so there the adapter is the
// one that refuses it (R2-P5 Fable F3). A further PBKDFParamRequest of the
// handshake ends the pairing at the router, counted, before it reaches the
// adapter (TestRepeatedPBKDFParamRequestEndsThePairing, review II
// R2-P6-sec-0304).
func TestPaseRefusalEndsTheTranscript(t *testing.T) {
	t.Parallel()
	prover, err := spake2.NewProver(20202021, failureCountTestSalt(), failureCountTestIterations, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProver: %v", err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatalf("GeneratePake1: %v", err)
	}
	goodPake1 := spake2.EncodePake1(pA)
	badPake1 := spake2.EncodePake1(make([]byte, 10)) // wrong pA length
	badPake3 := spake2.EncodePake3(make([]byte, 10)) // wrong cA length
	request := buildTestPBKDFParamRequest(t, bytes.Repeat([]byte{0x22}, spake2.PBKDFRandomSize))
	refusedRequest := buildTestPBKDFParamRequestPasscodeID(t, bytes.Repeat([]byte{0x22}, spake2.PBKDFRandomSize), 1)

	type step struct {
		opcode  uint8
		payload []byte
	}
	for _, tc := range []struct {
		name     string
		provider bool
		refusal  []step // after a well-formed PBKDFParamRequest
		wantPake int32  // verifiers built over the whole run
	}{
		{"singleton: a refused Pake1", false, []step{{mrp.SCOpcodePake1, badPake1}}, 1},
		{"singleton: a refused Pake3", false, []step{{mrp.SCOpcodePake1, goodPake1}, {mrp.SCOpcodePake3, badPake3}}, 1},
		{"singleton: a refused PBKDFParamRequest", false, []step{{mrp.SCOpcodePBKDFParamRequest, refusedRequest}}, 0},
		{"singleton: a Pake3 without a Pake1", false, []step{{mrp.SCOpcodePake3, badPake3}}, 0},
		{"provider: a refused Pake1", true, []step{{mrp.SCOpcodePake1, badPake1}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			var built atomic.Int32
			a := NewPaseAdapterWithFactory(newVerifierFactory(t, &built))
			a.SetPBKDFParams(failureCountTestIterations, failureCountTestSalt(), 1)
			a.randomSource = func() [spake2.PBKDFRandomSize]byte { return [spake2.PBKDFRandomSize]byte{0x11} }
			if tc.provider {
				b.AttachPaseHandlerProvider(func(HandshakeKey) PaseHandler { return a })
			} else {
				b.AttachPaseHandler(a)
			}
			const exchangeID = uint16(7)
			if err := b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodePBKDFParamRequest, exchangeID, false, 0), request); err != nil {
				t.Fatalf("PBKDFParamRequest: %v", err)
			}
			for _, s := range tc.refusal {
				_ = b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(s.opcode, exchangeID, false, 0), s.payload)
			}
			if got := b.paseFailures.Load(); got != 1 {
				t.Fatalf("paseFailures = %d after the refusal, want 1", got)
			}
			_ = b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodePake1, exchangeID, false, 0), goodPake1)
			if got := built.Load(); got != tc.wantPake {
				t.Fatalf("verifiers built = %d, want %d: the Pake1 after the refusal was computed against the ended handshake's transcript", got, tc.wantPake)
			}
		})
	}
}

// TestPaseConfirmedPake3EndsTheTranscript: a confirmed Pake3 ends the
// handshake as a refusal does — the PBKDFParamRequest/Response transcript
// it captured is dropped with the verifier, so a later Pake1 on the
// adapter has no context to run against (R2-P5 Fable F6).
func TestPaseConfirmedPake3EndsTheTranscript(t *testing.T) {
	t.Parallel()
	a := newPaseAdapterWithVerifier(t)
	if _, _, err := paseRoundTrip(t, a); err != nil {
		t.Fatalf("PASE round-trip: %v", err)
	}
	a.mu.Lock()
	req, resp, v := len(a.pbkdfReqBytes), len(a.pbkdfRespBytes), a.verifier
	a.mu.Unlock()
	if req != 0 || resp != 0 || v != nil {
		t.Fatalf("after a confirmed Pake3: transcript %d+%d bytes, verifier %v; want none", req, resp, v != nil)
	}
	a.mu.Lock()
	_, err := a.computePaseContextLocked()
	a.mu.Unlock()
	if err == nil {
		t.Fatal("a Pake1 context could still be derived after the handshake ended")
	}
}
