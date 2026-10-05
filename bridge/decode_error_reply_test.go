// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestUndecodableRequestIsAnsweredWithAStatusResponse pins that an IM
// request the bridge cannot decode is answered — a StatusResponse — rather
// than left for the controller to time out, as matter.js
// InteractionMessenger.handleRequest answers any error with
// StatusResponseError.of(error)?.code ?? FAILURE. TC-ACL-2.3 waited ten
// seconds for the answer to a write it had sent.
func TestUndecodableRequestIsAnsweredWithAStatusResponse(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 91
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	for i, op := range []uint8{im.OpcodeReadRequest, im.OpcodeWriteRequest, im.OpcodeInvokeRequest, im.OpcodeSubscribeRequest} {
		request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
			Opcode: op, ExchangeID: 0x2200 + uint16(i), Initiator: true,
		}, []byte{0x16, 0x18}) // an array where the request struct belongs
		_ = b.dispatch(context.Background(), request, peerAddr)
		proto, payload, ok := readOneIMDatagram(t, peerConn, peerSess, 2*time.Second)
		if !ok || proto.Opcode != im.OpcodeStatusResponse {
			t.Fatalf("opcode %#x: no StatusResponse to an undecodable request (got %#x, %v)", op, proto.Opcode, ok)
		}
		if len(payload) == 0 {
			t.Fatalf("opcode %#x: empty StatusResponse", op)
		}
	}
}
