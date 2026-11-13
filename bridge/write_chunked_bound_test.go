// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// chunkedWriteTestChunk dispatches one chunk of a write on exchange and
// returns the reply's opcode and, for a StatusResponse, its status.
func chunkedWriteTestChunk(t *testing.T, b *Bridge, exchange uint16, more bool, attribute uint32) (opcode uint8, status im.StatusCode) {
	t.Helper()
	proto := message.ProtocolHeader{
		ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeWriteRequest,
		ExchangeID: exchange, Initiator: true,
	}.Marshal()
	payload := encodeWriteChunkedTestRequest(t, false, false, more, im.ConcreteAttributePath{
		Endpoint: unreliabilityTestEndpoint, Cluster: 0x0006, Attribute: attribute,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	got := dispatchWriteChunkedTestRequest(t, b, secureTestDatagram(t, b, proto, payload))
	_, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	rproto, _, err := message.UnmarshalProtocolHeader(got[hdrLen:])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if rproto.Opcode == im.OpcodeStatusResponse {
		return rproto.Opcode, decodeWriteChunkedStatusResponse(t, got)
	}
	return rproto.Opcode, im.StatusSuccess
}

// TestChunkedWritesOfOneSessionAreBounded (review II, R2-P1-req-03, owner
// decision 30): a chunked write holds the attribute of its last chunk
// until its final chunk — taken before the ACL check, as matter.js takes
// it — so the number of chunked writes one session may have open is
// bounded. The write that would open one more is answered Busy before
// anything is held or written; the further chunks of an open write and a
// write that ends in its first chunk are not refused, and a finished write
// frees its place.
func TestChunkedWritesOfOneSessionAreBounded(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)

	for i := range maxChunkedWritesPerSession {
		if op, _ := chunkedWriteTestChunk(t, b, uint16(0x40+i), true, 0x4000+uint32(i)); op != im.OpcodeWriteResponse {
			t.Fatalf("chunked write %d: first chunk answered with opcode %#x, want WriteResponse", i, op)
		}
	}
	op, status := chunkedWriteTestChunk(t, b, 0x60, true, 0x4010)
	if op != im.OpcodeStatusResponse || status != im.StatusBusy {
		t.Fatalf("the chunked write beyond the bound: opcode %#x status %v, want StatusResponse Busy", op, status)
	}
	if _, held := b.routing.writeTxs.Load(mrp.ExchangeKey{SessionID: paseTestSessionID, ExchangeID: 0x60}); held {
		t.Error("the refused chunked write keeps a transaction")
	}
	// A write that ends in its first chunk opens nothing and is served.
	if op, _ := chunkedWriteTestChunk(t, b, 0x61, false, 0x4011); op != im.OpcodeWriteResponse {
		t.Errorf("an unchunked write at the bound answered with opcode %#x, want WriteResponse", op)
	}
	// An open write goes on: its middle and final chunks are served.
	if op, _ := chunkedWriteTestChunk(t, b, 0x40, true, 0x4000); op != im.OpcodeWriteResponse {
		t.Errorf("the middle chunk of an open write answered with opcode %#x, want WriteResponse", op)
	}
	if op, _ := chunkedWriteTestChunk(t, b, 0x40, false, 0x4000); op != im.OpcodeWriteResponse {
		t.Errorf("the final chunk of an open write answered with opcode %#x, want WriteResponse", op)
	}
	// Its place is free: a new chunked write runs to its end.
	for i, more := range []bool{true, true, false} {
		if op, _ := chunkedWriteTestChunk(t, b, 0x62, more, 0x4012); op != im.OpcodeWriteResponse {
			t.Fatalf("chunk %d of a new chunked write after one finished: opcode %#x, want WriteResponse", i, op)
		}
	}
}
