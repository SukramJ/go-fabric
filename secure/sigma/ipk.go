// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
)

// GroupSecurityInfo is the HKDF info string every Matter group-key
// derivation runs under, the fabric IPK included.
//
// Verbatim from matter.js
// packages/protocol/src/groups/FabricGroups.ts:15
// (`export const GROUP_SECURITY_INFO = Bytes.fromString("GroupKey v1.0")`).
const GroupSecurityInfo = "GroupKey v1.0"

// OperationalIPKSize is the length of a derived operational IPK in bytes.
//
// It is matter.js's `CRYPTO_SYMMETRIC_KEY_LENGTH`
// (packages/general/src/crypto/Crypto.ts:31 = 16), which is the default
// output length of `createHkdfKey`
// (packages/general/src/crypto/StandardCrypto.ts:156-160) — and
// `Fabric.create` takes that default rather than passing a length
// (packages/protocol/src/fabric/Fabric.ts:140-144).
const OperationalIPKSize = 16

// DeriveOperationalIPK turns the raw IPK a commissioner supplies in
// AddNOC.IPKValue into the per-fabric *operational* IPK that this package's
// handshake actually keys on — [Identity.IPK] and the first argument of
// [ComputeDestinationID].
//
//	operationalIPK = HKDF-SHA256(
//	    ikm  = rawIPK,             // 16 bytes, AddNOC.IPKValue
//	    salt = compressedFabricID, // 8 bytes, big-endian
//	    info = "GroupKey v1.0",
//	    L    = 16,
//	)
//
// Every input is taken from matter.js HEAD (f07365a8d), not from the
// specification prose:
//
//   - the algorithm, ikm and salt from `Fabric.create`
//     (packages/protocol/src/fabric/Fabric.ts:140-144), which calls
//     `crypto.createHkdfKey(config.identityProtectionKey,
//     Bytes.fromBigInt(globalId, 8), GROUP_SECURITY_INFO)`. `globalId` is
//     the compressed fabric id and `Bytes.fromBigInt(_, 8)` renders it
//     big-endian into 8 bytes, which is the byte order this module already
//     stores it in;
//   - the info string from [GroupSecurityInfo];
//   - SHA-256 and the 16-byte length from
//     packages/general/src/crypto/StandardCrypto.ts:156-172, see
//     [OperationalIPKSize].
//
// Deriving it is not optional and not cosmetic. Matter Core §4.13.2.5 makes
// the IPK the leading prefix of every CASE Sigma HKDF salt, so feeding the
// raw AddNOC value straight into the handshake produces a different S2K than
// the initiator computed, and a commissioner rejects Sigma2 with
// SecureChannel/INVALID_PARAMETER. Hosts had to hand-write this derivation
// from a doc comment before it was exported; it is security-relevant key
// material and belongs next to the code that consumes it.
func DeriveOperationalIPK(rawIPK []byte, compressedFabricID [8]byte) ([OperationalIPKSize]byte, error) {
	var out [OperationalIPKSize]byte
	if len(rawIPK) != OperationalIPKSize {
		return out, fmt.Errorf("sigma: raw IPK length %d, want %d", len(rawIPK), OperationalIPKSize)
	}
	derived, err := hkdf.Key(sha256.New, rawIPK, compressedFabricID[:], GroupSecurityInfo, OperationalIPKSize)
	if err != nil {
		return out, fmt.Errorf("sigma: derive operational IPK: %w", err)
	}
	copy(out[:], derived)
	return out, nil
}
