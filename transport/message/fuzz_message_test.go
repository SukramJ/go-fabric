// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package message

import (
	"reflect"
	"testing"
)

// FuzzUnmarshalHeader drives [UnmarshalHeader] with arbitrary bytes.
//
// This is the first code any inbound datagram touches: the message
// header is cleartext, is parsed before a session is resolved, and is
// therefore reachable by anyone who can send a UDP packet to the port.
// Asserted: no panic, the reported consumption stays inside the buffer,
// and — for headers that decode — the encode/decode round-trip holds,
// i.e. re-marshalling and re-decoding reproduces the same typed header.
//
// The round-trip deliberately compares the DECODED forms rather than the
// bytes. [Header.SecurityFlags] rebuilds the flags byte from the typed
// fields, so the reserved bits 4-2 of a received frame do not survive
// Marshal by design (they are preserved for AEAD purposes in
// [Header.Raw], which is excluded from the comparison for that reason).
// A byte-level assertion would be red on reserved bits without any
// semantic loss having occurred.
func FuzzUnmarshalHeader(f *testing.F) {
	// Known-good: the exact wire shapes the package's own round-trip
	// tests pin — unsecured, unicast with source + dest, group dest, and
	// a privacy-flagged header carrying a message extension.
	f.Add(Header{SessionID: 0, MessageCounter: 0xCAFEBABE, SessionType: SessionUnsecured}.Marshal())
	f.Add(Header{
		SessionID: 0x1234, MessageCounter: 42,
		HasSourceNodeID: true, SourceNodeID: 0x0011223344556677,
		DestSize: DestNodeID, DestNodeID: 0x8899AABBCCDDEEFF,
	}.Marshal())
	f.Add(Header{
		MessageCounter: 1, SessionType: SessionGroup, HasSourceNodeID: true, SourceNodeID: 5,
		DestSize: DestGroup, DestGroupID: 0xABCD,
	}.Marshal())
	f.Add(Header{
		SessionID: 7, MessageCounter: 9, Privacy: true, SessionType: SessionGroup,
		HasSourceNodeID: true, SourceNodeID: 5, DestSize: DestGroup, DestGroupID: 1,
		HasExtension: true, MessageExtension: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}.Marshal())

	// Deliberately malformed.
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})                                                // shorter than the 8-byte fixed part
	f.Add([]byte{0x10, 0, 0, 0, 0, 0, 0, 0})                                             // non-zero version nibble
	f.Add([]byte{0x03, 0, 0, 0, 0, 0, 0, 0})                                             // reserved DSIZ 0b11
	f.Add([]byte{0x00, 0, 0, 0x40, 0, 0, 0, 0})                                          // control-message bit
	f.Add([]byte{0x00, 0, 0, 0x02, 0, 0, 0, 0})                                          // session type 2
	f.Add([]byte{0x00, 0, 0, 0x20, 0, 0, 0, 0, 0xFF, 0xFF})                              // extension length past the buffer
	f.Add([]byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x02, 0x03})                           // dest node id truncated
	f.Add(Header{MessageCounter: 1, DestSize: DestGroup, DestGroupID: 0xABCD}.Marshal()) // unicast with a group destination
	f.Add([]byte{0x04, 0, 0, 0x01, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0})                  // group without a destination
	f.Add([]byte{0x02, 0, 0, 0x01, 0, 0, 0, 0, 0xCD, 0xAB})                              // group without a source node id

	f.Fuzz(func(t *testing.T, buf []byte) {
		h, n, err := UnmarshalHeader(buf)
		if err != nil {
			return
		}
		// What it accepts keeps matter.js's cross-field rules
		// (MessageCodec.ts:223-233).
		if h.SessionType == SessionUnsecured && h.DestSize == DestGroup {
			t.Fatalf("accepted a unicast header with a group destination: %+v", h)
		}
		if h.SessionType == SessionGroup && (h.DestSize == DestNone || !h.HasSourceNodeID) {
			t.Fatalf("accepted a group header without a destination or a source: %+v", h)
		}
		if n < 0 || n > len(buf) {
			t.Fatalf("UnmarshalHeader consumed %d of %d bytes", n, len(buf))
		}
		if len(h.Raw) != n {
			t.Fatalf("Header.Raw is %d bytes but %d were consumed", len(h.Raw), n)
		}

		again, n2, err := UnmarshalHeader(h.Marshal())
		if err != nil {
			t.Fatalf("re-decoding a marshalled header failed: %v (header=%+v)", err, h)
		}
		h.Raw, again.Raw = nil, nil // decode-only AAD cache, not semantic state
		if !reflect.DeepEqual(again, h) {
			t.Fatalf("header round-trip lost state (%d bytes in, %d out):\nfirst  %+v\nsecond %+v", n, n2, h, again)
		}
	})
}

// FuzzUnmarshalProtocolHeader mirrors [FuzzUnmarshalHeader] for the
// protocol header, which sits inside the encrypted payload but is parsed
// on every decrypted frame — including duplicates, whose plaintext the
// MRP layer peeks at specifically to synthesise an ack.
func FuzzUnmarshalProtocolHeader(f *testing.F) {
	f.Add(ProtocolHeader{Initiator: true, Opcode: 0x20, ExchangeID: 1, ProtocolID: 0x0001}.Marshal())
	f.Add(ProtocolHeader{HasAck: true, AckCounter: 0xDEADBEEF, Opcode: 0x10, ExchangeID: 0xFFFF}.Marshal())
	f.Add(ProtocolHeader{HasVendorID: true, VendorID: 0xFFF1, ProtocolID: 0x0002, NeedsAck: true}.Marshal())
	f.Add(ProtocolHeader{HasSecuredExt: true, SecuredExtension: []byte{1, 2, 3}}.Marshal())

	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00})                               // shorter than the 4-byte base
	f.Add([]byte{0x10, 0x00, 0x00, 0x00})                         // V flag with no vendor id
	f.Add([]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x00})             // A flag with no ack counter
	f.Add([]byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF}) // secured ext past the buffer

	f.Fuzz(func(t *testing.T, buf []byte) {
		h, n, err := UnmarshalProtocolHeader(buf)
		if err != nil {
			return
		}
		if n < 0 || n > len(buf) {
			t.Fatalf("UnmarshalProtocolHeader consumed %d of %d bytes", n, len(buf))
		}
		again, _, err := UnmarshalProtocolHeader(h.Marshal())
		if err != nil {
			t.Fatalf("re-decoding a marshalled protocol header failed: %v (header=%+v)", err, h)
		}
		if !reflect.DeepEqual(again, h) {
			t.Fatalf("protocol-header round-trip lost state:\nfirst  %+v\nsecond %+v", h, again)
		}
	})
}

// FuzzMarshalPacket drives the outbound encoder [Header.MarshalPacket]
// with arbitrary header fields. Asserted: no panic; a header it refuses
// breaks one of the encode constraints (the two of encodePacket, a unicast
// header with a group destination); a header it encodes
// decodes back to the same typed header, and a group header keeps its
// source node id and group id (matter.js MessageCodec.ts:481-504).
func FuzzMarshalPacket(f *testing.F) {
	f.Add(uint16(0x1234), uint32(7), true, uint64(0x1122334455667788), uint8(DestGroup), uint64(0), uint16(0x0101), uint8(SessionGroup), true, false)
	f.Add(uint16(1), uint32(1), false, uint64(0), uint8(DestNodeID), uint64(9), uint16(0), uint8(SessionUnsecured), false, false)
	f.Add(uint16(1), uint32(1), true, uint64(1), uint8(DestNone), uint64(0), uint16(0), uint8(SessionGroup), true, false)
	f.Add(uint16(1), uint32(1), true, uint64(1), uint8(DestGroup), uint64(0), uint16(2), uint8(SessionGroup), true, true)

	f.Fuzz(func(t *testing.T, session uint16, counter uint32, hasSource bool, source uint64, dsiz uint8, destNode uint64, destGroup uint16, sessionType uint8, privacy, ext bool) {
		h := Header{
			SessionID: session, MessageCounter: counter, HasSourceNodeID: hasSource,
			DestSize: DestSize(dsiz % 3), SessionType: SessionType(sessionType % 2), Privacy: privacy, HasExtension: ext,
		}
		if hasSource {
			h.SourceNodeID = source
		}
		switch h.DestSize {
		case DestNone:
		case DestNodeID:
			h.DestNodeID = destNode
		case DestGroup:
			h.DestGroupID = destGroup
		}
		b, err := h.MarshalPacket()
		if err != nil {
			groupBroken := h.SessionType == SessionGroup && (h.DestSize != DestGroup || !h.HasSourceNodeID)
			unicastGroup := h.SessionType == SessionUnsecured && h.DestSize == DestGroup
			if !ext && !groupBroken && !unicastGroup {
				t.Fatalf("refused a valid header %+v: %v", h, err)
			}
			return
		}
		got, n, err := UnmarshalHeader(b)
		if err != nil || n != len(b) {
			t.Fatalf("encoded header does not decode: %v (%d of %d)", err, n, len(b))
		}
		got.Raw = nil
		if !reflect.DeepEqual(got, h) {
			t.Fatalf("round trip:\nin  %+v\nout %+v", h, got)
		}
	})
}
