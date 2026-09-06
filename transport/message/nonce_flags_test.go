// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package message

import "testing"

// TestNonceSecurityFlagsKeepsReservedWireBits — the byte that feeds the
// AEAD nonce is the byte as received, reserved bits included. The typed
// accessor SecurityFlags() drops bits 4-2 by construction (it re-encodes
// the fields Marshal writes), which is right for the wire and wrong for
// the nonce: the peer built its nonce from the byte it sent. Mirrors
// matter.js MessageCodec.ts:45 keeping securityFlags as a pure data
// field for the nonce.
func TestNonceSecurityFlagsKeepsReservedWireBits(t *testing.T) {
	wire := Header{MessageCounter: 1, Privacy: true}.Marshal()
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
