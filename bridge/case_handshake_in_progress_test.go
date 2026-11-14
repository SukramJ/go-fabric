// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestMessagesUnderAHandshakeInProgressChangeNothing: between the Sigma2 and
// the Sigma3 of a CASE handshake, a Sigma1 under its initiator node id and
// exchange that begins nothing, or a Sigma2_Resume under them, from another
// sender, is acknowledged and not answered: the handshake's record stays,
// its Sigma2 stays in retransmission to the controller, and the
// controller's Sigma3 completes the handshake — through a
// PerExchangeCaseProvider and through the singleton handler alike (review
// II, R2-P4-fr-sec-03). In matter.js both meet the handshake's live
// exchange with the Sigma2 unacknowledged and fail there before anything
// reaches the CASE server or the retransmission timer
// (packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0).
func TestMessagesUnderAHandshakeInProgressChangeNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		viaProvider bool
		opcode      uint8
	}{
		{"a Sigma1 that begins nothing, through a provider", true, mrp.SCOpcodeSigma1},
		{"a Sigma1 that begins nothing, through the singleton handler", false, mrp.SCOpcodeSigma1},
		{"a Sigma2_Resume, through a provider", true, mrp.SCOpcodeSigma2Resume},
		{"a Sigma2_Resume, through the singleton handler", false, mrp.SCOpcodeSigma2Resume},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := beginCASE(t, newUnsecuredRig(t), clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			want := c.controller.conn.LocalAddr().String()
			other := c.otherPeer(t)
			if err := other.send(tc.opcode, completedCaseExchange, []byte{0x15, 0x18}, nil); err != nil {
				t.Fatal(err)
			}
			if got := pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange); len(got) != 1 || got[0] != want {
				t.Fatalf("after opcode %#x from another sender the replies waiting for an acknowledgement go to %v, want the Sigma2 to %s", tc.opcode, got, want)
			}
			if !c.r.b.ackTracker.Owes(0, completedCaseExchange, false) {
				t.Fatal("no acknowledgement owed for the message from another sender — a reply carried it")
			}
			if !c.recordHeld() {
				t.Fatal("the message from another sender ended the handshake's record")
			}
			if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
				t.Fatal(err)
			}
			report := c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the controller's Sigma3")
			if got := c.sessions.Load(); got != 1 {
				t.Fatalf("%d sessions installed, want 1", got)
			}
			if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &report); err != nil {
				t.Fatal(err)
			}
			if !tc.viaProvider {
				expectNoHandshakeState(t, c.r.b, "after the success report was acknowledged")
			}
		})
	}
}

// TestSigma1OverARecordIsJudgedOnce: while a Sigma1 of other bytes is
// judged over the record of a handshake, a copy of it is dropped as a
// replay, as a copy of a first Sigma1 is; once it began nothing, the
// record is as it was and the same bytes are judged again (review II,
// R2-P4-fr-sec-03).
func TestSigma1OverARecordIsJudgedOnce(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	key := HandshakeKey{InitiatorNodeID: 0xC0DE, ExchangeID: 7}
	first, other := [32]byte{1}, [32]byte{2}
	if replay, over := b.markSigma1Replied(key, first); replay || over {
		t.Fatalf("first Sigma1: replay %v over a record %v, want neither", replay, over)
	}
	if replay, over := b.markSigma1Replied(key, other); replay || !over {
		t.Fatalf("a Sigma1 of other bytes: replay %v over a record %v, want judged over the record", replay, over)
	}
	if replay, _ := b.markSigma1Replied(key, other); !replay {
		t.Fatal("a copy of the Sigma1 being judged was not dropped as a replay")
	}
	b.sigma1BeganNothingOverRecord(key, other)
	b.mu.RLock()
	rec := b.sigma1Replied[key]
	b.mu.RUnlock()
	if rec.hash != first || rec.judging {
		t.Fatalf("after a Sigma1 that began nothing the record is %+v, want the first Sigma1's, unmarked", rec)
	}
	if replay, over := b.markSigma1Replied(key, other); replay || !over {
		t.Fatalf("the same other bytes again: replay %v over a record %v, want judged again", replay, over)
	}
}
