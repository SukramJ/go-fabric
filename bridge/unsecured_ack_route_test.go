// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// The acknowledgement of an unsecured message goes to the initiator that sent
// it, whatever another initiator sends on the same exchange id meanwhile:
// matter.js keeps each initiator's exchanges, and their acknowledgements, in
// that initiator's own unsecured session
// (packages/protocol/src/protocol/ExchangeManager.ts:308-312 and :401 at
// db021da0); review II, R2-P3-lan-dev-07.
func TestAckOfAnUnsecuredMessageGoesToItsInitiator(t *testing.T) {
	t.Parallel()
	for _, other := range []bool{false, true} {
		name := "alone"
		if other {
			name = "another initiator on the same exchange id"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newUnsecuredRig(t)
			commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
			stranger := newPipelinePeer(t, r.b, 0xBAD0BAD0)
			const exchange = uint16(0x0303)
			// A reliable message the node answers with nothing but an ack:
			// an unhandled Secure-Channel opcode stands in for any
			// reliable message whose reply is not piggybacked in time.
			const opcode = uint8(0x7E)
			_ = commissioner.send(opcode, exchange, nil, nil)
			counter := commissioner.counter
			if other {
				_ = stranger.send(opcode, exchange, nil, nil)
				if !ackArrives(t, stranger.conn, stranger.counter) {
					t.Error("the other initiator's message was not acknowledged to it")
				}
			}
			r.b.RunAckPumpOnce(r.b.clk().Now().Add(time.Second))
			if !ackArrives(t, commissioner.conn, counter) {
				t.Fatalf("the acknowledgement of the commissioner's counter %d did not reach the commissioner", counter)
			}
		})
	}
}

// ackArrives reports whether conn receives a StandaloneAck for counter
// before the test's read deadline.
func ackArrives(t *testing.T, conn *net.UDPConn, counter uint32) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	buf := make([]byte, 1500)
	for {
		_ = conn.SetReadDeadline(deadline)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return false
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			continue
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			continue
		}
		if proto.Opcode == mrp.StandaloneAckOpcode && proto.HasAck && proto.AckCounter == counter {
			return true
		}
	}
}
