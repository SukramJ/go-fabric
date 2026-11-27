// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package setup

import "testing"

// TestVerhoeffParityMatterJS mirrors
// packages/general/test/math/VerhoeffTest.ts:10-16 at db91886c
// (computeChecksum "236" is 3): the check digit a manual pairing code
// closes with (verhoeffCheck).
func TestVerhoeffParityMatterJS(t *testing.T) {
	t.Parallel()
	if got, err := verhoeffCheck("236"); err != nil || got != 3 {
		t.Fatalf("verhoeffCheck(236) = %d, %v; want 3", got, err)
	}
}
