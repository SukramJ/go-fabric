// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

// TestCompressedFabricIDHkdfParityMatterJS mirrors
// packages/general/test/crypto/StandardCryptoTest.ts:92-100 at 862ad75f
// ("performs correct HKDF computation"): HKDF-SHA256 over a root public
// key without its 0x04 prefix, salt the fabric id 1 as eight big-endian
// bytes, info "CompressedFabric", eight bytes — the compressed fabric id.
func TestCompressedFabricIDHkdfParityMatterJS(t *testing.T) {
	t.Parallel()
	pub, _ := hex.DecodeString("04" + "dbc94ee08a5ee674b4c1bfa7b05bfd339faa0cd67853a10d367e9790a6d064af5ea3650da79a228adfaf771970f5f31cdc3f0ebde443640185a6e0f488f0a243")
	id, err := ca.CompressedFabricID(pub, 1)
	if err != nil || hex.EncodeToString(id[:]) != "ab4a2b4fba653117" {
		t.Fatalf("CompressedFabricID = %x, %v; want ab4a2b4fba653117", id, err)
	}
}
