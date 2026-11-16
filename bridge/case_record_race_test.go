// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/sha256"
	"testing"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestCompletedRecordHoldsWhileItsReportIsSentAgain: while the success
// report of a completed handshake is sent again for the retransmission of
// its Sigma3 — the old report abandoned, the new one not tracked yet — a
// liveness check of the record does not end it, so the next retransmitted
// Sigma3 still gets its report (review II, U4-fr-sec-1, the first item).
func TestCompletedRecordHoldsWhileItsReportIsSentAgain(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	key := scKey(0x31)
	hash := sha256.Sum256([]byte("Sigma1"))
	b.markSigma1Replied(key, hash)
	b.sigma1BeganHandshake(key, hash, false, true, false)
	b.sigma3Handled(key, sigma3Established, completedSigma3{digest: sha256.Sum256([]byte("Sigma3")), route: loopbackSrc(), bySingleton: true})
	read := func() sigma1Record {
		b.mu.RLock()
		defer b.mu.RUnlock()
		return b.sigma1Replied[key]
	}
	release := b.holdCompletedRecord(key)
	rec := read()
	if b.sigma1RecordOver(key, rec, nil) {
		t.Fatal("the record of a report being sent again reads as over")
	}
	b.endSigma1Record(key, rec)
	if _, held := b.completedHandshake(key); !held {
		t.Fatal("the record ended while its report was being sent again")
	}
	release()
	if !b.sigma1RecordOver(key, read(), nil) {
		t.Fatal("the record, its report sent and nothing tracked, does not read as over")
	}
}

// TestSigma1JudgedAcrossAHandlerSwapLeavesNoHandshake: a Sigma1 the
// singleton handler was judging when another handler was attached leaves
// no handshake behind once it began one on the replaced handler — as the
// swap ends the handshakes the replaced handler began — so its Sigma3 is
// acknowledged and not handed to a handler that never saw its Sigma1
// (review II, U4-fr-sec-1, the second item).
func TestSigma1JudgedAcrossAHandlerSwapLeavesNoHandshake(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	next := &recordingCaseHandler{sigma1RespCode: mrp.SCOpcodeSigma2, sigma1Resp: []byte{0xDD}}
	swapping := &swapDuringSigma1Handler{b: b, next: next}
	b.AttachCaseHandler(swapping)
	if err := b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodeSigma1, 0x32, false, 0), []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	b.mu.RLock()
	_, held := b.sigma1Replied[scKey(0x32)]
	b.mu.RUnlock()
	if held {
		t.Fatal("a Sigma1 the replaced handler began kept its record past the swap")
	}
}

// swapDuringSigma1Handler attaches next as the bridge's CASE handler while
// it judges a Sigma1, which it then answers with a Sigma2.
type swapDuringSigma1Handler struct {
	b    *Bridge
	next CaseHandler
}

func (h *swapDuringSigma1Handler) ProcessSigma1([]byte) (opcode uint8, payload []byte, err error) {
	h.b.AttachCaseHandler(h.next)
	return mrp.SCOpcodeSigma2, []byte{0xD1}, nil
}

func (h *swapDuringSigma1Handler) ProcessSigma3([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errHandshakeNotBegun
}

func (h *swapDuringSigma1Handler) ProcessSigma2Resume([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errHandshakeNotBegun
}
