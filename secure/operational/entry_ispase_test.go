// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package operational_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestEntryIsPASE_SurvivesAdoptFabricIndex pins the accessor the bridge
// reads to stamp the commissioning channel's auth mode. AddNOC adopts the
// PASE session onto the new fabric (matter.js
// packages/node/src/behaviors/operational-credentials/
// OperationalCredentialsServer.ts:266-270), so FabricIndex stops being 0
// while the session stays PASE — and matter.js keys the implicit
// Administer grant on exactly that (FabricAccessControl.ts:189-191).
func TestEntryIsPASE_SurvivesAdoptFabricIndex(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager()
	secret := make([]byte, 16)
	for i := range secret {
		secret[i] = byte(i + 3)
	}

	pase, err := m.OpenFromPase(1, 2, 0, secret)
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	if !pase.IsPASE() {
		t.Fatal("IsPASE() on a fresh PASE session = false, want true")
	}
	if err := m.AdoptFabricIndex(pase.SessionID, 2); err != nil {
		t.Fatalf("AdoptFabricIndex: %v", err)
	}
	if pase.FabricIndex() != 2 {
		t.Fatalf("FabricIndex after adopt = %d, want 2", pase.FabricIndex())
	}
	if !pase.IsPASE() {
		t.Fatal("IsPASE() after AdoptFabricIndex = false, want true")
	}

	// Negative control: a CASE session never reports PASE.
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i] = byte(i + 1)
		keys.R2IKey[i] = byte(i + 40)
	}
	caseSess, err := m.OpenFromSigma(2, 100, 200, keys)
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	if caseSess.IsPASE() {
		t.Fatal("IsPASE() on a CASE session = true, want false")
	}
}
