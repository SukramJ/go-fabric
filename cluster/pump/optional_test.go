// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
)

// TestOptionalForMapsTheSelection: OptionalFor serves an optional
// attribute of the selection, and one of AUTO's limits sets the whole
// group (the device kit's route from a declaration's attributes).
func TestOptionalForMapsTheSelection(t *testing.T) {
	if got := pump.OptionalFor([]uint32{pump.AttrPumpStatus}); got != pump.OptionalPumpStatus {
		t.Errorf("PumpStatus: %b", got)
	}
	if got := pump.OptionalFor([]uint32{pump.AttrMinConstSpeed, pump.AttrMaxConstSpeed}); got != pump.OptionalAutomaticLimits {
		t.Errorf("the limits: %b", got)
	}
	if got := pump.OptionalFor(nil); got != 0 {
		t.Errorf("nothing: %b", got)
	}
}
