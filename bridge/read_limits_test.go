// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for the bounds on one Read / Subscribe interaction:
// the path ceiling and the per-chunk StatusResponse wait. Lives in
// package bridge to reach the receive pipeline directly.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// decodeStatusResponsePayload reads the Status of an IM StatusResponse
// body.
func decodeStatusResponsePayload(t *testing.T, payload []byte) im.StatusCode {
	t.Helper()
	sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(payload))
	if err != nil {
		t.Fatalf("UnmarshalStatusResponseTLV: %v", err)
	}
	return sr.Status
}

// TestReadRequestAbovePathCeilingIsRejectedWithPathsExhausted pins the
// hard acceptance ceiling on read paths: matter.js
// InteractionServer.ts:79 MAX_READ_PATHS = 10 000, rejected at
// :366-369 with a top-level StatusResponse(PathsExhausted) before any
// path is expanded.
func TestReadRequestAbovePathCeilingIsRejectedWithPathsExhausted(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 79
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: make([]im.ConcreteAttributePath, im.MaxReadPaths+1)}.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatalf("ReadRequest.MarshalTLV: %v", err)
	}
	request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeReadRequest, ExchangeID: 0x2101, Initiator: true,
	}, payload)
	if err := b.dispatch(context.Background(), request, peerAddr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	proto, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok {
		t.Fatal("no reply reached the peer")
	}
	if proto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("reply opcode = %#x, want StatusResponse %#x (the oversized read was served instead of rejected)", proto.Opcode, im.OpcodeStatusResponse)
	}
	if got := decodeStatusResponsePayload(t, body); got != im.StatusPathsExhausted {
		t.Errorf("status = %v, want PathsExhausted", got)
	}
}

// TestSubscribeRequestAbovePathCeilingIsRejectedWithPathsExhausted is
// the Subscribe twin (InteractionServer.ts:80 MAX_SUBSCRIBE_PATHS,
// :600-604).
func TestSubscribeRequestAbovePathCeilingIsRejectedWithPathsExhausted(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 80
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	enc := tlv.NewEncoder()
	im.SubscribeRequest{
		MaxIntervalCeiling: 60,
		AttributeRequests:  make([]im.ConcreteAttributePath, im.MaxReadPaths+1),
	}.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatalf("SubscribeRequest.MarshalTLV: %v", err)
	}
	request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeSubscribeRequest, ExchangeID: 0x2102, Initiator: true,
	}, payload)
	if err := b.dispatch(context.Background(), request, peerAddr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	proto, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok {
		t.Fatal("no reply reached the peer")
	}
	if proto.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("reply opcode = %#x, want StatusResponse %#x (the oversized subscribe was served instead of rejected)", proto.Opcode, im.OpcodeStatusResponse)
	}
	if got := decodeStatusResponsePayload(t, body); got != im.StatusPathsExhausted {
		t.Errorf("status = %v, want PathsExhausted", got)
	}
}

// TestReadChunkLoopAbortsWhenThePeerNeverAnswers pins that a chunk the
// peer never answers ends the interaction instead of releasing the
// next chunk: matter.js's per-chunk waitForSuccess runs under the
// exchange timeout and a missing answer throws PeerMessageMissingError
// (MessageExchange.ts:829-858), abandoning the read. Falling through
// instead lets a silent peer hold one dispatch slot for every chunk of
// the whole report.
func TestReadChunkLoopAbortsWhenThePeerNeverAnswers(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 81
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = 200 * time.Millisecond
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeReadRequest, ExchangeID: 0x2103, Initiator: true,
	}, buildWildcardReadRequestPayload(t))

	done := make(chan error, 1)
	go func() { done <- b.dispatch(context.Background(), request, peerAddr) }()

	if _, _, ok := readOneIMDatagram(t, peerConn, peerSess, 2*time.Second); !ok {
		t.Fatal("no ReportData chunk reached the peer")
	}
	// The peer stays silent. Nothing more may follow on the exchange.
	if extra, _, got := readOneIMDatagram(t, peerConn, peerSess, time.Second); got {
		t.Errorf("a further datagram (opcode %#x) followed an unanswered chunk; the read must be aborted", extra.Opcode)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("dispatch returned nil for a read the peer never acknowledged; want the abort surfaced")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read dispatch still running 2 s after the first chunk went unanswered")
	}
}
