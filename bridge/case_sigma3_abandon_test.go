// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestSigma3TheHandlerDidNotTakeKeepsTheTrackedReport: a Sigma3 that reaches
// a handshake's handler once that handler has established its session — a
// garbage Sigma3 racing the one that completed it, before the router marked
// the record completed — is acknowledged and not answered, and it leaves
// the reply still waiting for its acknowledgement tracked: the replies are
// abandoned only for a Sigma3 the handler took (review II, N3-fixrrdev-1,
// N4-rwsub-3).
func TestSigma3TheHandlerDidNotTakeKeepsTheTrackedReport(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
	a := NewCaseAdapter(sigma.NewResponder(nil, nil, 0x2000))
	a.established = true
	b.AttachCaseHandlerProvider(func(HandshakeKey) CaseHandler { return a })
	const exchange = uint16(9)
	key := scKey(exchange)
	hash := sha256.Sum256([]byte("Sigma1"))
	b.markSigma1Replied(key, hash)
	b.sigma1BeganHandshake(key, hash, false, false, false)
	b.outboundReliable.trackUnsecured(77, key, []byte{0x01}, loopbackSrc(), time.Now(), nil)

	_ = b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodeSigma3, exchange, false, 0), []byte{0xEE})
	if got := pendingOnUnsecuredExchange(b, exchange); got != 1 {
		t.Fatalf("the tracked report after a Sigma3 the handler did not take: %d pending, want 1", got)
	}
}
