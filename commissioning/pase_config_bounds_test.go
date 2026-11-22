// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioning_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/commissioning"
)

// TestNewPASEResponderParameterBounds is the boundary table of
// NewPASEResponder's passcode range 1..99999998 and iteration range
// 1000..100000: the last refused and the first accepted value at each
// end, each with its own sentinel (R2-P5-tmut-11).
func TestNewPASEResponderParameterBounds(t *testing.T) {
	t.Parallel()
	salt := make([]byte, 16)
	for _, tc := range []struct {
		name       string
		passcode   uint32
		iterations int
		wantErr    error
	}{
		{"passcode 0", 0, 1000, commissioning.ErrPASEInvalidPasscode},
		{"passcode 1", 1, 1000, nil},
		{"passcode 99999998", 99999998, 1000, nil},
		{"passcode 99999999", 99999999, 1000, commissioning.ErrPASEInvalidPasscode},
		{"iterations 999", 20202021, 999, commissioning.ErrPASEInvalidIterations},
		{"iterations 1000", 20202021, 1000, nil},
		{"iterations 100000", 20202021, 100000, nil},
		{"iterations 100001", 20202021, 100001, commissioning.ErrPASEInvalidIterations},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, err := commissioning.NewPASEResponder(commissioning.PASEConfig{Passcode: tc.passcode, Salt: salt, Iterations: tc.iterations})
			if tc.wantErr == nil {
				if err != nil || r == nil {
					t.Fatalf("refused: %v", err)
				}
				r.Close()
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
