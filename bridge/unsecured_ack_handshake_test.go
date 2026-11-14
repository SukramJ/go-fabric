// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestUnsecuredAckReachesItsOwnHandshakeOnly: on the unsecured session an
// acknowledgement clears a message of its own handshake — the initiator and
// the exchange it names — and no other: a message counter acknowledged by
// another initiator, or on another exchange, leaves the message waiting for
// its own acknowledgement. matter.js acts on an acknowledgement inside the
// exchange the message belongs to (packages/protocol/src/protocol/MessageExchange.ts:521-537
// at db021da0), found in the unsecured session of the initiator the message
// names (ExchangeManager.ts:306-308 and :398-401).
func TestUnsecuredAckReachesItsOwnHandshakeOnly(t *testing.T) {
	t.Parallel()
	const exchange = uint16(0x0505)
	t.Run("a reply of the responder", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name       string
			fromOther  bool
			onExchange uint16
			wantKept   bool
		}{
			{"acknowledged by its initiator", false, exchange, false},
			{"acknowledged by another initiator", true, exchange, true},
			{"acknowledged on another exchange", false, exchange + 1, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				r := newUnsecuredRig(t)
				r.b.AttachCaseHandler(sigma2ThenSuccessHandler{})
				commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
				stranger := newPipelinePeer(t, r.b, 0xBAD0BAD0)
				if err := commissioner.send(mrp.SCOpcodeSigma1, exchange, []byte{0x15, 0x18}, nil); err != nil {
					t.Fatalf("Sigma1: %v", err)
				}
				_, counter, _ := commissioner.recv(mrp.SCOpcodeSigma2, "the Sigma2")
				acker := commissioner
				if tc.fromOther {
					acker = stranger
				}
				if err := acker.send(mrp.StandaloneAckOpcode, tc.onExchange, nil, &counter); err != nil {
					t.Fatalf("ack: %v", err)
				}
				if kept := pendingOnUnsecuredExchange(r.b, exchange) == 1; kept != tc.wantKept {
					t.Errorf("the Sigma2 still waits for its acknowledgement: %v, want %v", kept, tc.wantKept)
				}
			})
		}
	})
	t.Run("a message of a handshake this node opened", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name      string
			fromOther bool
			wantKept  bool
		}{
			{"acknowledged by its responder", false, false},
			{"acknowledged for another initiator", true, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				r := newUnsecuredRig(t)
				h := startInitiatedHandshake(t, r)
				ctx, sigma1Hdr, sigma1 := h.ctx, h.sigma1, h.exchange
				dest := sigma1Hdr.SourceNodeID
				if tc.fromOther {
					dest ^= 1
				}
				ack := message.ProtocolHeader{
					Opcode: mrp.StandaloneAckOpcode, ExchangeID: sigma1.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
					HasAck: true, AckCounter: sigma1Hdr.MessageCounter,
				}
				if err := r.b.dispatch(ctx, responderDatagram(600, dest, ack, nil), r.peer.LocalAddr()); err != nil {
					t.Fatalf("dispatch(ack): %v", err)
				}
				if kept := len(r.tracked(t, sigma1.ExchangeID)) == 1; kept != tc.wantKept {
					t.Errorf("the Sigma1 still waits for its acknowledgement: %v, want %v", kept, tc.wantKept)
				}
				h.cancel()
				<-h.done
			})
		}
	})
}
