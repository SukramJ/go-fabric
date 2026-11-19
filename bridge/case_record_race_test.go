// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

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
	return 0, nil, errSwapHandlerTakesNoSigma3
}

func (h *swapDuringSigma1Handler) ProcessSigma2Resume([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errSwapHandlerTakesNoSigma3
}

// errSwapHandlerTakesNoSigma3 is swapDuringSigma1Handler's failure of the
// messages it does not take.
var errSwapHandlerTakesNoSigma3 = errors.New("bridge test: the swapping handler takes Sigma1 only")
