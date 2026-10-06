// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"testing"
	"time"
)

// suiteTimeout bounds the broad suite. It is generous on purpose: the legs
// are fast when the daemon is healthy (tens of milliseconds per operation
// over the shared session), and a hung leg should say where it hung rather
// than be killed by the Makefile's outer -timeout.
const suiteTimeout = 15 * time.Minute

// TestChipToolSuite is the broad real-controller suite: one reference daemon,
// commissioned once, one interactive chip-tool session on its fabric, and
// every leg below run over that session in order. The order matters only
// where a leg changes shared state (a second fabric, a restart); those run
// last.
func TestChipToolSuite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), suiteTimeout)
	defer cancel()
	f := newFixture(ctx, t)

	legs := []struct {
		name string
		run  func(*testing.T, *fixture)
	}{
		{"datamodel", testDataModelSweep},
	}
	for _, leg := range legs {
		start := time.Now()
		t.Run(leg.name, func(t *testing.T) { leg.run(t, f) })
		t.Logf("leg %s: %s", leg.name, time.Since(start).Round(time.Millisecond))
	}
}
