// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestMessagesUnderAHandshakeInProgressChangeNothing: between the Sigma2 and
// the Sigma3 of a CASE handshake, a Sigma1 under its initiator node id and
// exchange that begins nothing — one that does not decode, or a well-formed
// one whose destination id names no fabric of the node — or a
// Sigma2_Resume under them, from another sender, does not acknowledge the
// Sigma2 and is refused on receive, not acknowledged and not answered
// (review II, phase 7, decision 277): the handshake's record stays,
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
		foreign     bool
	}{
		{"a Sigma1 that does not decode, through a provider", true, mrp.SCOpcodeSigma1, false},
		{"a Sigma1 that does not decode, through the singleton handler", false, mrp.SCOpcodeSigma1, false},
		{"a Sigma1 for no fabric of the node, through a provider", true, mrp.SCOpcodeSigma1, true},
		{"a Sigma1 for no fabric of the node, through the singleton handler", false, mrp.SCOpcodeSigma1, true},
		{"a Sigma2_Resume, through a provider", true, mrp.SCOpcodeSigma2Resume, false},
		{"a Sigma2_Resume, through the singleton handler", false, mrp.SCOpcodeSigma2Resume, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := beginCASE(t, newUnsecuredRig(t), clocktest.New(time.Unix(1_700_000_000, 0)), tc.viaProvider)
			want := c.controller.conn.LocalAddr().String()
			other := c.otherPeer(t)
			payload := []byte{0x15, 0x18}
			if tc.foreign {
				payload = foreignSigma1(t)
			}
			err := other.send(tc.opcode, completedCaseExchange, payload, nil)
			c.expectRefused(t, err, errPreviousAckMissing, 1, "a message from another sender")
			if got := pendingDestsOnUnsecuredExchange(c.r.b, completedCaseExchange); len(got) != 1 || got[0] != want {
				t.Fatalf("after opcode %#x from another sender the replies waiting for an acknowledgement go to %v, want the Sigma2 to %s", tc.opcode, got, want)
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

// TestAcknowledgingMessagesUnderAHandshakeInProgressChangeNothing: the
// messages of TestMessagesUnderAHandshakeInProgressChangeNothing from
// another sender, when they pass the receive rules — one that acknowledges
// the Sigma2 (session-0 counters travel in the clear), or one that arrives
// after the controller's standalone acknowledgement of it — reach the
// router and are acknowledged and not answered (owner decision 70): the
// handshake's record stays, and the controller's Sigma3 completes the
// handshake, through a PerExchangeCaseProvider and through the singleton
// handler alike (review II, phase 7, Fable's read A3).
func TestAcknowledgingMessagesUnderAHandshakeInProgressChangeNothing(t *testing.T) {
	t.Parallel()
	for _, viaProvider := range []bool{true, false} {
		for _, opcode := range []uint8{mrp.SCOpcodeSigma1, mrp.SCOpcodeSigma2Resume} {
			for _, afterAck := range []bool{false, true} {
				name := fmt.Sprintf("provider=%t opcode=%#x after-standalone-ack=%t", viaProvider, opcode, afterAck)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					c := beginCASE(t, newUnsecuredRig(t), clocktest.New(time.Unix(1_700_000_000, 0)), viaProvider)
					other := c.otherPeer(t)
					var ack *uint32
					if afterAck {
						if err := c.controller.send(mrp.StandaloneAckOpcode, completedCaseExchange, nil, &c.sigma2Counter); err != nil {
							t.Fatal(err)
						}
					} else {
						ack = &c.sigma2Counter
					}
					if err := other.send(opcode, completedCaseExchange, []byte{0x15, 0x18}, ack); err != nil {
						t.Fatalf("a message that passes the receive rules: %v", err)
					}
					c.expectNoReply(t, 0, "a message from another sender")
					if !c.recordHeld() {
						t.Fatal("the message from another sender ended the handshake's record")
					}
					if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
						t.Fatal(err)
					}
					c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the controller's Sigma3")
					if got := c.sessions.Load(); got != 1 {
						t.Fatalf("%d sessions installed, want 1", got)
					}
				})
			}
		}
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
	if rec.hash != first || rec.judged != nil {
		t.Fatalf("after a Sigma1 that began nothing the record is %+v, want the first Sigma1's, unmarked", rec)
	}
	if replay, over := b.markSigma1Replied(key, other); replay || !over {
		t.Fatalf("the same other bytes again: replay %v over a record %v, want judged again", replay, over)
	}
}

// foreignSigma1 is a well-formed Sigma1 whose destination id names a fabric
// under a root of its own, which no node of the tests holds.
func foreignSigma1(t *testing.T) []byte {
	t.Helper()
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ini, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity: newCaseTestIdentity(t, 0xAAAA, 1, newCaseTestIPK(t)), Verifier: caseTestVerifier{},
		SessionID: 0x1003, PeerNodeID: 0xBBBB,
		RootPublicKey: elliptic.Marshal(elliptic.P256(), root.X, root.Y), //nolint:staticcheck // the uncompressed point the CASE destination id hashes
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := ini.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	return s1
}
