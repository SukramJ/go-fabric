// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
)

// HoldProvers records the SPAKE2+ prover of every PASE handshake started
// until the test ends. A test that calls it must not be parallel: it
// replaces the package's newProver seam.
func HoldProvers(t *testing.T) *[]*spake2.Prover {
	t.Helper()
	var held []*spake2.Prover
	prev := newProver
	newProver = func(passcode uint32, salt []byte, iterations int, idA, idB, context []byte) (*spake2.Prover, error) {
		p, err := prev(passcode, salt, iterations, idA, idB, context)
		if err == nil {
			held = append(held, p)
		}
		return p, err
	}
	t.Cleanup(func() { newProver = prev })
	return &held
}
