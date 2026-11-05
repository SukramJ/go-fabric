// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import "testing"

// TestEphemeralScalarBoundParityMatterJS mirrors
// packages/general/test/crypto/Spake2pTest.ts:65-84 at 862ad75f
// ("samples bounded by the group order n, not the field prime p"): the
// ephemeral scalar is drawn below the P-256 group order n
// (defaultRandomScalar draws rand.Int below curve().Params().N), never
// zero. matter.js captures the bound it hands its random source; the Go
// test draws scalars and holds each in [1, n).
func TestEphemeralScalarBoundParityMatterJS(t *testing.T) {
	t.Parallel()
	n := curve().Params().N
	for range 256 {
		k, err := defaultRandomScalar()
		if err != nil {
			t.Fatal(err)
		}
		if k.Sign() <= 0 || k.Cmp(n) >= 0 {
			t.Fatalf("scalar %x outside [1, n)", k)
		}
	}
}
