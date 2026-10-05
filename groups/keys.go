// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/secure/channel"
)

// Key-derivation labels, Matter §4.17.2. Mirrors matter.js
// packages/protocol/src/groups/FabricGroups.ts GROUP_SECURITY_INFO and
// packages/protocol/src/groups/KeySets.ts GROUP_KEY_INFO.
const (
	groupSecurityInfo = "GroupKey v1.0"
	groupKeyInfo      = "GroupKeyHash"
)

// keySize is the length of an epoch key and of every key derived from one.
const keySize = 16

// ErrEpochKeyLength is returned for an epoch key that is not 16 bytes.
var ErrEpochKeyLength = errors.New("groups: epoch key must be 16 bytes")

// OperationalKey derives the operational group key of an epoch key:
// HKDF-SHA256 with the fabric's compressed fabric id (8 bytes, big-endian,
// as the commissioner computed it) as salt and "GroupKey v1.0" as info.
// Mirrors matter.js FabricGroups.setFromGroupKeySet
// (`createHkdfKey(epochKey, globalId, GROUP_SECURITY_INFO)`).
func OperationalKey(epochKey []byte, compressedFabricID [8]byte) ([]byte, error) {
	if len(epochKey) != keySize {
		return nil, fmt.Errorf("%w: got %d", ErrEpochKeyLength, len(epochKey))
	}
	out, err := hkdf.Key(sha256.New, epochKey, compressedFabricID[:], groupSecurityInfo, keySize)
	if err != nil {
		return nil, fmt.Errorf("groups: operational key: %w", err)
	}
	return out, nil
}

// SessionID derives the group session id of an operational group key: the
// two-byte HKDF-SHA256 "GroupKeyHash" over the key with an empty salt,
// read big-endian. Mirrors matter.js KeySets.sessionIdFromKey.
func SessionID(operationalKey []byte) (uint16, error) {
	hash, err := hkdf.Key(sha256.New, operationalKey, nil, groupKeyInfo, 2)
	if err != nil {
		return 0, fmt.Errorf("groups: session id: %w", err)
	}
	return binary.BigEndian.Uint16(hash), nil
}

// PrivacyKey derives the privacy key of an operational group key, the
// same derivation a unicast session uses (Matter §4.9.1). Mirrors matter.js
// MessagePrivacy.deriveKey as FabricGroups.setFromGroupKeySet calls it.
func PrivacyKey(operationalKey []byte) ([]byte, error) {
	return channel.DerivePrivacyKey(operationalKey)
}

// MulticastAddress returns the IPv6 multicast address a fabric's group is
// reached on under the PerGroupID policy: FF35:0040:FD<FabricID>00:<GroupID>
// — a unicast-prefix-based, site-scoped address carrying the 64-bit fabric
// id and the 16-bit group id (Matter §2.5.6.2). Mirrors matter.js
// packages/protocol/src/groups/Groups.ts:multicastAddress.
func MulticastAddress(fabricID uint64, groupID uint16) net.IP {
	ip := make(net.IP, net.IPv6len)
	binary.BigEndian.PutUint16(ip[0:2], 0xFF35)
	binary.BigEndian.PutUint16(ip[2:4], 0x0040)
	ip[4] = 0xFD
	binary.BigEndian.PutUint64(ip[5:13], fabricID)
	ip[13] = 0x00
	binary.BigEndian.PutUint16(ip[14:16], groupID)
	return ip
}
