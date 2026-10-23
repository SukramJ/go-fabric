// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermostat_test

import (
	"testing"

	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// TestCelsiusToInt16 holds the encoder to its clamp: centi-degrees rounded,
// 32766 at most (32767 is the null), -27315 (absolute zero) at least.
func TestCelsiusToInt16(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		celsius float64
		want    int16
	}{
		{21.5, 2150}, {-4.25, -425}, {0.004, 0}, {327.67, 32766}, {1000, 32766}, {-273.15, -27315}, {-300, -27315},
	} {
		if got := thermostatdef.CelsiusToInt16(c.celsius); got != c.want {
			t.Errorf("CelsiusToInt16(%v) = %d, want %d", c.celsius, got, c.want)
		}
	}
}
