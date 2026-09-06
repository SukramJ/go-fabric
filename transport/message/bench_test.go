// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package message

// Benchmarks for the header codec on the inbound datagram path.
//
// Every datagram a bridge receives is parsed here before anything else looks
// at it — bridge/receive.go:dispatch calls UnmarshalHeader on the raw bytes
// and UnmarshalProtocolHeader on the decrypted body — and that happens for
// traffic the bridge ends up discarding too: duplicates, retransmits, frames
// for sessions it does not hold. The parse therefore runs more often than
// any handler behind it, which is what makes it worth a floor of its own.
//
// The fixtures are marshalled once, outside the timed region; the timed loop
// only decodes.

import "testing"

// Sinks. Package-level so the decode cannot be optimised away.
var (
	benchHeader   Header
	benchProto    ProtocolHeader
	benchHdrBytes []byte
)

// benchSecuredHeaderBytes returns the wire bytes of the header shape a
// commissioned controller actually sends: a unicast session id, a message
// counter, and a source node id. The source-node-id branch is included
// deliberately — it is the branch a real fabric exercises, while the bare
// 8-byte header only appears on unsecured commissioning traffic.
func benchSecuredHeaderBytes() []byte {
	return Header{
		SessionID:       0x1234,
		MessageCounter:  0x00A0B0C0,
		HasSourceNodeID: true,
		SourceNodeID:    0x00DE000000000001,
		SessionType:     SessionUnsecured,
	}.Marshal()
}

// benchProtocolHeaderBytes returns a Protocol Header carrying an
// acknowledgement, which is the common case on an established exchange:
// every reliable message after the first piggybacks an ack counter, so the
// ack branch is the hot one rather than the exception.
func benchProtocolHeaderBytes() []byte {
	return ProtocolHeader{
		ExchangeID: 0x0042,
		ProtocolID: 0x0001, // Interaction Model
		Opcode:     0x05,   // ReportData
		HasAck:     true,
		AckCounter: 0x00A0B0BF,
		NeedsAck:   true,
	}.Marshal()
}

// BenchmarkUnmarshalHeader measures the first parse every inbound datagram
// pays. Watch the allocation count: the decoder copies the received header
// bytes into Header.Raw so the AEAD additional-authenticated-data survives
// an in-place decrypt of the caller's buffer, and that copy is the one
// allocation this path is allowed to make.
func BenchmarkUnmarshalHeader(b *testing.B) {
	buf := benchSecuredHeaderBytes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h, _, err := UnmarshalHeader(buf)
		if err != nil {
			b.Fatalf("UnmarshalHeader: %v", err)
		}
		benchHeader = h
	}
}

// BenchmarkUnmarshalProtocolHeader measures the second parse, which runs on
// the decrypted body of every datagram that survives session lookup and
// decryption.
func BenchmarkUnmarshalProtocolHeader(b *testing.B) {
	buf := benchProtocolHeaderBytes()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p, _, err := UnmarshalProtocolHeader(buf)
		if err != nil {
			b.Fatalf("UnmarshalProtocolHeader: %v", err)
		}
		benchProto = p
	}
}

// BenchmarkMarshalHeader measures the outbound counterpart. It is here
// because the encode is not a mirror image of the decode in cost — it
// allocates the datagram prefix for every message the bridge sends,
// including each chunk of a large subscription report — and a change to the
// header layout touches both directions at once.
func BenchmarkMarshalHeader(b *testing.B) {
	h := Header{
		SessionID:       0x1234,
		MessageCounter:  0x00A0B0C0,
		HasSourceNodeID: true,
		SourceNodeID:    0x00DE000000000001,
		SessionType:     SessionUnsecured,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchHdrBytes = h.Marshal()
	}
}
