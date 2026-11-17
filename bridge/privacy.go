// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"encoding/binary"
	"errors"
)

// secFlagPrivacyBit and secFlagSessionTypeBits mirror the transport
// package's unexported Security Flags layout (P bit 7, session type in
// bits 1-0). Re-declared here so the bridge layer can inspect a still-
// masked frame without decoding it.
const (
	secFlagPrivacyBit      = 0x80
	secFlagSessionTypeBits = 0x03
)

var (
	// errPrivacyOnUnsecured is a datagram of the unsecured session (PASE,
	// CASE before their session exists) that carries the P bit.
	errPrivacyOnUnsecured = errors.New("bridge: P bit set on SessionID=0 (PASE) — spec violation")
	// errPrivacyOnUnicast is a unicast datagram that carries the P bit.
	errPrivacyOnUnicast = errors.New("bridge: P bit set on a unicast message — dropped (privacy is defined for group messages only)")
)

// refuseUnicastPrivacy refuses a datagram on the unicast path that carries
// the P bit of its Security Flags (byte 3, unprotected, as are the session
// id in bytes 1-2): privacy enhancements are defined for group messages
// only (Matter §4.9), and a group datagram leaves [Bridge.dispatch] before
// this point — its privacy mask is removed on the group path
// (groups/decode.go). A unicast frame carrying the P bit is invalid and is
// dropped rather than unmasked and handed to a unicast session, as
// matter.js does (MessageCodec.ts:250-253, hasPrivacyEnhancements off a
// group session is an UnexpectedDataError). A frame without the P bit, or
// too short to carry the Security Flags, passes: the header decode reports
// the latter more clearly. Review II, R2-P6-err-0312: the unmask of a
// group frame this function held could not be reached from the receive
// path, and went.
func refuseUnicastPrivacy(buf []byte) error {
	if len(buf) < 4 || buf[3]&secFlagPrivacyBit == 0 {
		return nil
	}
	if binary.LittleEndian.Uint16(buf[1:3]) == 0 {
		return errPrivacyOnUnsecured
	}
	return errPrivacyOnUnicast
}
