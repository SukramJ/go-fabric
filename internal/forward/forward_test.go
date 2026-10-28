// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package forward_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/internal/forward"
)

// TestEnable: a feature is off until enabled, stays on while any enabler
// holds it, and a restore runs once.
func TestEnable(t *testing.T) {
	if forward.Enabled(forward.PQCPhase1) {
		t.Fatal("on by default")
	}
	a := forward.Enable(forward.PQCPhase1)
	b := forward.Enable(forward.PQCPhase1)
	a()
	a()
	if !forward.Enabled(forward.PQCPhase1) {
		t.Fatal("off while the second enabler holds it")
	}
	b()
	if forward.Enabled(forward.PQCPhase1) {
		t.Fatal("on after every restore")
	}
}
