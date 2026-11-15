// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"errors"
	"testing"
)

// TestNewVerifierContextIterationBounds is the boundary table of
// NewVerifierContext's PBKDF iteration range [IterationsMin, IterationsMax]:
// the last refused and the first accepted value at each end. PBKDF itself
// refuses only a count <= 0, so this check is the only thing between a
// caller and a 1-iteration verifier (R2-P5-tmut-11).
func TestNewVerifierContextIterationBounds(t *testing.T) {
	t.Parallel()
	salt := make([]byte, 16)
	for _, tc := range []struct {
		iterations int
		ok         bool
	}{
		{IterationsMin - 1, false},
		{IterationsMin, true},
		{IterationsMax, true},
		{IterationsMax + 1, false},
	} {
		vc, err := NewVerifierContext(20202021, salt, tc.iterations)
		switch {
		case tc.ok && (err != nil || vc == nil):
			t.Errorf("iterations %d: refused: %v", tc.iterations, err)
		case !tc.ok && !errors.Is(err, ErrInvalidPasscode):
			t.Errorf("iterations %d: err = %v, want ErrInvalidPasscode", tc.iterations, err)
		}
	}
}
