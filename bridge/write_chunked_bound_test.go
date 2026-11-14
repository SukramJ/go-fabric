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

// TestClosedSessionReleasesItsChunkedWrites (review II, R2-P1-dr-req-02): a
// session's open chunked writes end with the session — on SessionClosed
// and on a peer-loss eviction, where its chunk waits and timed deadlines
// go — so the attributes they held are writable from another session at
// once, not only after writeTxTTL, and a session given the same id starts
// with none open. A chunk whose handling overlapped the close gives back
// what it took when it ends. matter.js releases the holds when
// handleWriteRequest leaves its scope (InteractionServer.ts:460-469 at
// db021da0), which the session's force-close ends.
func TestClosedSessionReleasesItsChunkedWrites(t *testing.T) {
	t.Parallel()
	attr := func(i uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: i, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	for _, tc := range []struct {
		name  string
		close func(b *Bridge, sid uint16)
	}{
		{"SessionClosed", func(b *Bridge, sid uint16) { b.SessionClosed(sid) }},
		{"evictSession", func(b *Bridge, sid uint16) { b.evictSession(sid, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			now := b.clk().Now()
			write := func(key mrp.ExchangeKey, p im.ConcreteAttributePath, more bool) (im.StatusCode, bool) {
				t.Helper()
				tx := b.routing.writeTransaction(key, more, now)
				if tx == nil {
					return 0, false
				}
				resp := im.HandleWriteRequest(im.WithWriteTransaction(t.Context(), tx), holdDispatcher{}, im.WriteRequest{
					MoreChunkedMessages: more,
					Writes:              []im.AttributeWrite{{Path: p, Value: im.AttributeValue{Value: "x"}}},
				})
				b.routing.endWriteChunk(key, tx, more)
				if len(resp.Responses) != 1 {
					t.Fatalf("responses %+v, want one", resp.Responses)
				}
				return resp.Responses[0].Status.Status, true
			}
			const sid, other = 0x77, 0x78
			for i := range uint32(maxChunkedWritesPerSession) {
				if _, opened := write(mrp.ExchangeKey{SessionID: sid, ExchangeID: uint16(1 + i)}, attr(i), true); !opened {
					t.Fatalf("chunked write %d refused", i)
				}
			}
			// A chunk in hand when the session closes: its transaction was
			// looked up before the close, its holds are taken after it.
			late := mrp.ExchangeKey{SessionID: sid, ExchangeID: 1}
			lateTx := b.routing.writeTransaction(late, true, now)
			tc.close(b, sid)
			im.HandleWriteRequest(im.WithWriteTransaction(t.Context(), lateTx), holdDispatcher{}, im.WriteRequest{
				MoreChunkedMessages: true,
				Writes:              []im.AttributeWrite{{Path: attr(100), Value: im.AttributeValue{Value: "x"}}},
			})
			b.routing.endWriteChunk(late, lateTx, true)

			for i := range uint32(maxChunkedWritesPerSession) {
				if s, _ := write(mrp.ExchangeKey{SessionID: other, ExchangeID: uint16(0x100 + i)}, attr(i), false); s != im.StatusSuccess {
					t.Errorf("attribute %d held by the closed session's chunked write: another session's write answered %v, want SUCCESS", i, s)
				}
			}
			if s, _ := write(mrp.ExchangeKey{SessionID: other, ExchangeID: 0x200}, attr(100), false); s != im.StatusSuccess {
				t.Errorf("the attribute a chunk overlapping the close took: another session's write answered %v, want SUCCESS", s)
			}
			for i := range uint32(maxChunkedWritesPerSession) {
				if _, opened := write(mrp.ExchangeKey{SessionID: sid, ExchangeID: uint16(0x300 + i)}, attr(0x10+i), true); !opened {
					t.Fatalf("a session reusing the id: chunked write %d refused, want the count to start at zero", i)
				}
			}
		})
	}
}
