// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"encoding/hex"
	"testing"
)

// TestGroupSessionIDDerivationParityMatterJS mirrors
// packages/nodejs/test/crypto/NodeJsCryptoTest.ts:174-183 at 862ad75f
// ("validate group session id derivation"): HKDF-SHA256 "GroupKeyHash"
// over the operational group key a6f5…d960 with an empty salt, two bytes
// read big-endian, is 0xB9F7 (SessionID).
func TestGroupSessionIDDerivationParityMatterJS(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString("a6f5306baf6d050af23ba4bd6b9dd960")
	id, err := SessionID(key)
	if err != nil || id != 0xB9F7 {
		t.Fatalf("SessionID = 0x%04X, %v; want 0xB9F7", id, err)
	}
}
