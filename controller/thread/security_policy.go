// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import (
	"encoding/binary"
	"fmt"
)

// SecurityPolicy is the MeshCoP Security Policy TLV (type 12): the key
// rotation time in hours and the raw 16-bit policy flags, whose bit layout
// depends on the Thread revision (Thread spec §8.10.1.15).
//
// Mirrors matter.js packages/protocol/src/thread/SecurityPolicy.ts:10-21
// (SecurityPolicy). Both fields are uint16, so the range checks of
// SecurityPolicy.encode (:36-41) are the Go type.
type SecurityPolicy struct {
	RotationTime uint16
	Flags        uint16
}

// DecodeSecurityPolicy reads the four-byte TLV value: big-endian rotation
// time, then big-endian flags. Any other length fails with
// [ErrThreadDataset].
//
// Mirrors matter.js packages/protocol/src/thread/SecurityPolicy.ts:24-33
// (SecurityPolicy.decode).
func DecodeSecurityPolicy(value []byte) (SecurityPolicy, error) {
	if len(value) != 4 {
		return SecurityPolicy{}, fmt.Errorf("%w: security policy must be 4 bytes, got %d", ErrThreadDataset, len(value))
	}
	return SecurityPolicy{
		RotationTime: binary.BigEndian.Uint16(value),
		Flags:        binary.BigEndian.Uint16(value[2:]),
	}, nil
}

// Encode is the four-byte TLV value of p.
//
// Mirrors matter.js packages/protocol/src/thread/SecurityPolicy.ts:35-48
// (SecurityPolicy.encode).
func (p SecurityPolicy) Encode() []byte {
	return binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(make([]byte, 0, 4), p.RotationTime), p.Flags)
}
