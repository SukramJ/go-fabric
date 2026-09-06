// SPDX-License-Identifier: MIT
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
	f.Add(Header{MessageCounter: 1, DestSize: DestGroup, DestGroupID: 0xABCD}.Marshal())
	f.Add(Header{
		SessionID: 7, MessageCounter: 9, Privacy: true, SessionType: SessionGroup,
		HasExtension: true, MessageExtension: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}.Marshal())

	// Deliberately malformed.
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})                      // shorter than the 8-byte fixed part
	f.Add([]byte{0x10, 0, 0, 0, 0, 0, 0, 0})                   // non-zero version nibble
	f.Add([]byte{0x03, 0, 0, 0, 0, 0, 0, 0})                   // reserved DSIZ 0b11
	f.Add([]byte{0x00, 0, 0, 0x40, 0, 0, 0, 0})                // control-message bit
	f.Add([]byte{0x00, 0, 0, 0x02, 0, 0, 0, 0})                // session type 2
	f.Add([]byte{0x00, 0, 0, 0x20, 0, 0, 0, 0, 0xFF, 0xFF})    // extension length past the buffer
	f.Add([]byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x02, 0x03}) // dest node id truncated

	f.Fuzz(func(t *testing.T, buf []byte) {
		h, n, err := UnmarshalHeader(buf)
		if err != nil {
			return
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
