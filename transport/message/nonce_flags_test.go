// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package message

import (
	"bytes"
	"testing"
)

// TestNonceSecurityFlagsKeepsReservedWireBits — the byte that feeds the
// AEAD nonce is the byte as received, reserved bits included. The typed
// accessor SecurityFlags() drops bits 4-2 by construction (it re-encodes
// the fields Marshal writes), which is right for the wire and wrong for
// the nonce: the peer built its nonce from the byte it sent. Mirrors
// matter.js MessageCodec.ts:45 keeping securityFlags as a pure data
// field for the nonce.
func TestNonceSecurityFlagsKeepsReservedWireBits(t *testing.T) {
	wire := Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 1, Privacy: true}.Marshal()
	wire[3] |= 0x08 // reserved bit 3
	out, _, err := UnmarshalHeader(wire)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	if got := out.NonceSecurityFlags(); got != wire[3] {
		t.Errorf("NonceSecurityFlags() = 0x%02X, received byte = 0x%02X", got, wire[3])
	}
	if got := out.SecurityFlags(); got == wire[3] {
		t.Errorf("SecurityFlags() = 0x%02X retained a reserved bit; the wire encoder must not echo it", got)
	}
	// A header built in memory has no wire byte: both accessors agree.
	mem := Header{MessageCounter: 2, SessionType: SessionGroup}
	if mem.NonceSecurityFlags() != mem.SecurityFlags() {
		t.Errorf("in-memory header: NonceSecurityFlags() = 0x%02X, SecurityFlags() = 0x%02X", mem.NonceSecurityFlags(), mem.SecurityFlags())
	}
}

// TestHeaderAADKeepsReservedWireBits — the AEAD's additional data is the
// header as received, not a re-encoding of it. A header whose security
// flags carry a reserved bit re-encodes without it, so a receiver that
// authenticated Marshal() instead of Header.Raw would fail the tag of
// every frame its peer sent with such a bit — and accept a frame whose
// reserved bits were altered in flight. Mirrors matter.js authenticating
// the raw received header bytes, as Header.AAD's comment cites
// (R2-P5-tmut-18).
func TestHeaderAADKeepsReservedWireBits(t *testing.T) {
	wire := Header{SessionID: 0x1234, MessageCounter: 7, HasSourceNodeID: true, SourceNodeID: 1}.Marshal()
	wire[3] |= 0x08 // reserved bit 3 of the security flags
	out, n, err := UnmarshalHeader(wire)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	if bytes.Equal(out.Marshal(), wire[:n]) {
		t.Fatal("precondition: the re-encoded header kept the reserved bit, the case holds nothing")
	}
	if got := out.AAD(); !bytes.Equal(got, wire[:n]) {
		t.Errorf("AAD = % X, want the received header % X", got, wire[:n])
	}
}
