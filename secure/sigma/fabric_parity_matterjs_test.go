// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma_test

// A port of matter.js packages/protocol/test/fabric/FabricTest.ts at
// 862ad75f: the CASE destination id of a fabric against the matter.js
// vectors.
//
// matter.js computes it with Fabric.currentDestinationIdFor; the Go
// computation, shared by the initiator and both responders, is
// sigma.ComputeDestinationID over the fabric's operational IPK, root public
// key, fabric id and the destination node id.

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

func fabricTestDestinationID(t *testing.T, ipkHex, randomHex, rootPubHex string, fabricID, nodeID uint64) string {
	t.Helper()
	var ipk [16]byte
	copy(ipk[:], mustHex(t, ipkHex))
	var random [sigma.RandomSize]byte
	copy(random[:], mustHex(t, randomHex))
	got := sigma.ComputeDestinationID(ipk, random, mustHex(t, rootPubHex), fabricID, nodeID)
	return hex.EncodeToString(got[:])
}

func TestFabricGetDestinationID(t *testing.T) {
	t.Parallel()

	t.Run("generates the correct destination ID", func(t *testing.T) {
		t.Parallel()
		// ProtocolMocks.Fabric.defaults (ProtocolMocks.ts:73-88) and
		// FabricTest.ts:18-19.
		got := fabricTestDestinationID(t,
			"9bc61cd9c62a2df6d64dfcaa9dc472d4",
			"7e171231568dfa17206b3accf8faec2f4d21b580113196f47c7c4deb810a73dc",
			"044a9f42b1ca4840d37292bbc7f6a7e11e22200c976fc900dbc98a7a383a641cb8254a2e56d4e295a847943b4e3897c4a773e930277b4d9fbede8a052686bfacfa",
			0x2906c908d115d362, 0xcd5544aa7b13ef14)
		if want := "dc35dd5fc9134cc5544538c9c3fc4297c1ec3370c839136a80e10796451d4c53"; got != want {
			t.Fatalf("destination id = %s, want %s", got, want)
		}
	})

	t.Run("generates the correct destination ID 3", func(t *testing.T) {
		t.Parallel()
		// FabricTest.ts:25-30.
		got := fabricTestDestinationID(t,
			"6624ed691e9b53b1f286d98117918c3a",
			"0b2a71876d3d090d37cb5286168ab9be0d2e7e0ccbedc1f55331b8a8051ee02f",
			"04d89eb7e3f3226d0918f4b85832457bb9981bca7aaef58c18fb5ec07525e472b2bd1617fb75ee41bd388f94ae6a6070efc896777516a5c54aff74ec0804cdde9d",
			0x0000000000000001, 0x0000000000000055)
		if want := "b1ce1a45fd930831203024286cd609ec4f9bbe71ed8e10c2c4cab0be49a87e8a"; got != want {
			t.Fatalf("destination id = %s, want %s", got, want)
		}
	})
}
