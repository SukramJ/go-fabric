// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestGroupSessionArmOfTheIMGateSendsNothing pins the one behaviour a
// Read, Subscribe or Timed request on a group session has (review II,
// W2-wire-08): it is dropped without a reply. Group datagrams leave the
// receive path before handleIMOpcode (dispatchGroupMessage, which drops
// them silently), so the gate's group arm is not reached in production —
// but it answered StatusResponse(InvalidAction), which neither reference
// does: matter.js throws InvalidAction and sends a status only
// `if (!isGroupSession)` (packages/protocol/src/interaction/
// InteractionMessenger.ts:354-358, :458-460 at db021da0), chip returns
// without one (src/app/InteractionModelEngine.cpp:1059-1066 at
// 6170af84). The header here carries a session id the bridge holds keys
// for, so a reply — were one built — would reach the peer.
func TestGroupSessionArmOfTheIMGateSendsNothing(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 92
	b := newStartedBridge(t)
	peerConn, _, peerAddr := chunkTestPeer(t, b, sessionID)

	for _, opcode := range []uint8{im.OpcodeReadRequest, im.OpcodeSubscribeRequest, im.OpcodeTimedRequest} {
		hdr := &message.Header{SessionID: sessionID, SessionType: message.SessionGroup, MessageCounter: 7}
		proto := message.ProtocolHeader{
			ProtocolID: im.InteractionModelProtocolID, Opcode: opcode, ExchangeID: 0x2200 + uint16(opcode), Initiator: true,
		}
		if got := classifyIMOpcode(proto.Opcode, hdr.SessionType, hdr.SessionID); got != imGateRejectGroupSession {
			t.Fatalf("opcode %#x: gate = %v, want imGateRejectGroupSession", opcode, got)
		}
		if err := b.handleIMOpcode(context.Background(), peerAddr, hdr, proto, nil); err != nil {
			t.Errorf("opcode %#x: handleIMOpcode = %v, want nil (dropped)", opcode, err)
		}
	}
	if n := datagramsWithin(t, peerConn, 300*time.Millisecond); n != 0 {
		t.Errorf("%d datagram(s) answered a request on a group session, want none", n)
	}
}

// datagramsWithin counts the datagrams that reach conn until wait passed
// without one.
func datagramsWithin(t *testing.T, conn *net.UDPConn, wait time.Duration) int {
	t.Helper()
	buf := make([]byte, udp.MaxDatagramSize)
	n := 0
	for {
		if err := conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		if _, _, err := conn.ReadFromUDP(buf); err != nil {
			return n
		}
		n++
	}
}
