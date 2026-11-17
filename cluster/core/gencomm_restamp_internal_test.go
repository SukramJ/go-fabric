// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "testing"

// SetCurrentFabric while no fail-safe is armed writes nothing (review II,
// R2-P6-fsm-1024).
func TestSetCurrentFabricWhileDisarmedWritesNoIndex(t *testing.T) {
	t.Parallel()
	g := &GeneralCommissioning{}
	g.SetCurrentFabric(3)
	if g.failSafeFabricIndex != 0 || g.failSafeFabricAdded {
		t.Errorf("after a re-stamp while disarmed: index %d, added %v; want 0, false", g.failSafeFabricIndex, g.failSafeFabricAdded)
	}
}
