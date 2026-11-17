// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// RotatingIDUniqueIDLength is the least byte length of the Unique-ID input
// of the rotating-device-id derivation: connectedhomeip's
// CHIP_DEVICE_CONFIG_ROTATING_DEVICE_ID_UNIQUE_ID_LENGTH 16, "The value
// should be 16-bytes or longer" (src/include/platform/CHIPDeviceConfig.h:380-384
// at 6170af84), and its kMinRotatingDeviceIDUniqueIDLength
// (src/include/platform/ConfigurationManager.h:80-81). The node draws it
// at random once and keeps it in its store
// (node.Commissioning.RotatingDeviceID).
const RotatingIDUniqueIDLength = 16

// rotatingIDSuffixLength is the number of trailing SHA-256 bytes
// included in the rotating identifier. chip
// `setup_payload/AdditionalDataPayloadGenerator.h:38
// RotatingDeviceId::kHashSuffixLength = 16`.
const rotatingIDSuffixLength = 16

// GenerateRotatingID builds the rotating-device-id hex string:
//
//	RDI = uint16_le(LifetimeCounter) || SHA-256(UniqueID || uint16_le(LifetimeCounter))[-16:]
//	RotatingID(hex) = uppercase(hex(RDI))
//
// Follows connectedhomeip src/setup_payload/AdditionalDataPayloadGenerator.cpp:81-111
// (generateRotatingDeviceIdAsBinary): the identifier is the 2-byte
// lifetime counter followed by the last 16 bytes of SHA-256(unique id ‖
// counter, little-endian); the TXT value RI is those 18 bytes as 36
// uppercase hex characters (:113-127). It is the `RI` TXT key of the
// `_matterc._udp` commissionable record.
//
// uniqueID must be at least [RotatingIDUniqueIDLength] bytes; the
// function returns the empty string when uniqueID is too short so
// callers can suppress the `RI` TXT key on pre-init bridges.
//
// References:
//   - chip `src/setup_payload/AdditionalDataPayloadGenerator.cpp:81`
//     (generateRotatingDeviceIdAsBinary)
//   - chip `src/setup_payload/AdditionalDataPayloadGenerator.cpp:113`
//     (generateRotatingDeviceIdAsHexString)
//   - Matter Core Spec §5.4.2.4 "Rotating Device Identifier"
func GenerateRotatingID(uniqueID []byte, lifetimeCounter uint16) string {
	if len(uniqueID) < RotatingIDUniqueIDLength {
		return ""
	}
	var counterLE [2]byte
	binary.LittleEndian.PutUint16(counterLE[:], lifetimeCounter)

	h := sha256.New()
	h.Write(uniqueID)
	h.Write(counterLE[:])
	digest := h.Sum(nil)

	const total = 2 + rotatingIDSuffixLength
	var binBuf [total]byte
	copy(binBuf[:2], counterLE[:])
	copy(binBuf[2:], digest[len(digest)-rotatingIDSuffixLength:])

	out := make([]byte, total*2)
	hex.Encode(out, binBuf[:])
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

// MustGenerateRotatingID is a helper for tests / first-boot wiring
// that constructs a RotatingID from a fresh uniqueID. It panics when
// uniqueID is shorter than [RotatingIDUniqueIDLength] so the caller
// gets a clear failure mode at construction rather than a silent
// empty RI key.
func MustGenerateRotatingID(uniqueID []byte, lifetimeCounter uint16) string {
	if len(uniqueID) < RotatingIDUniqueIDLength {
		// invariant: uniqueID is the daemon's own locally-generated /
		// persisted commissionable-node identifier, never data parsed
		// off the wire — a short value here is a caller bug (e.g. a
		// truncated config seed), not something a remote peer can
		// trigger.
		panic(fmt.Sprintf("mdns: uniqueID length %d < %d (min)", len(uniqueID), RotatingIDUniqueIDLength))
	}
	return GenerateRotatingID(uniqueID, lifetimeCounter)
}

// DeriveUniqueIDFromIdentity computes a deterministic 16-byte
// UniqueID from an identity tuple (VendorID, ProductID, SerialNumber,
// optional NodeLabel): the first 16 bytes of
// SHA-256(vendor || product || serial || label).
//
// The value is no secret: anyone who knows the four inputs — a
// commissioner, any fabric's administrator, often the label on the
// device — computes it, and with it every RI it yields, so an RI built on
// it identifies the device instead of rotating (review II,
// R2-P6-sec-0207). A unique id for [GenerateRotatingID] is random, drawn
// once and persisted, as node.Commissioning.RotatingDeviceID does.
//
// Deprecated: use 16 random bytes drawn once and persisted as the unique
// id passed to [GenerateRotatingID] (node.Commissioning.RotatingDeviceID
// does both and advances a persisted lifetime counter). Removable in the
// next minor release.
func DeriveUniqueIDFromIdentity(vendorID, productID uint16, serialNumber, nodeLabel string) []byte {
	h := sha256.New()
	var idBytes [4]byte
	binary.BigEndian.PutUint16(idBytes[0:2], vendorID)
	binary.BigEndian.PutUint16(idBytes[2:4], productID)
	h.Write(idBytes[:])
	h.Write([]byte(serialNumber))
	h.Write([]byte{0x00})
	h.Write([]byte(nodeLabel))
	out := h.Sum(nil)
	return out[:RotatingIDUniqueIDLength]
}
