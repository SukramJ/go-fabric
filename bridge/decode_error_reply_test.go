// SPDX-License-Identifier: Apache-2.0
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

// TestUnsupportedOpcodeOpeningAnExchangeIsAnsweredInvalidAction pins the
// byte cases of review 2026-11 W2-wire-02: a StatusResponse, a response
// opcode or an unassigned opcode as the first message of an exchange is
// answered StatusResponse(InvalidAction). matter.js
// packages/protocol/src/interaction/InteractionMessenger.ts:415-419,
// :431-461; chip src/app/InteractionModelEngine.cpp:1095-1099, :1112-1115.
func TestUnsupportedOpcodeOpeningAnExchangeIsAnsweredInvalidAction(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 94
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	ops := []uint8{
		im.OpcodeStatusResponse, im.OpcodeSubscribeResponse, im.OpcodeReportData,
		im.OpcodeWriteResponse, im.OpcodeInvokeResponse, 0x0B, 0xFF,
	}
	for i, op := range ops {
		payload := []byte{0x15, 0x18}
		if op == im.OpcodeStatusResponse {
			payload = []byte{0x15, 0x24, 0x00, 0x00, 0x18} // Status: Success
		}
		request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
			Opcode: op, ExchangeID: 0x2400 + uint16(i), Initiator: true,
		}, payload)
		_ = b.dispatch(context.Background(), request, peerAddr)
		proto, body, ok := readOneIMDatagram(t, peerConn, peerSess, 2*time.Second)
		if !ok || proto.Opcode != im.OpcodeStatusResponse {
			t.Fatalf("opcode %#x: no StatusResponse (got %#x, %v)", op, proto.Opcode, ok)
		}
		if status := decodeStatusResponseCode(t, body); status != im.StatusInvalidAction {
			t.Errorf("opcode %#x: status = %v, want InvalidAction", op, status)
		}
	}
}
