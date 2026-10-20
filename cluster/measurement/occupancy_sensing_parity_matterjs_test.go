// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/occupancy-sensing/OccupancySensingServerTest.ts
// at 862ad75f, "instantiates" and "chooses correct defaults".
//
// matter.js builds OccupancySensingServer.with("Radar") on a MockEndpoint;
// the Go server has one feature selection, PassiveInfrared (every host
// motion sensor is PIR, definitions.go occupancyInst), so the cases run on
// it. The defaults case asserts the HoldTime-dependent
// PirUnoccupiedToOccupiedThreshold absent: matter.js clears every
// HoldTime dependent when HoldTime is unset (OccupancySensingServer.ts
// initialize, holdTimeDependencies), and the Go server serves no HoldTime
// and none of its dependents. "emits OccupancyChanged …" needs the
// OccupancyEvent feature the Go server cannot select (the register names
// why).
package measurement_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
)

// TestOccupancySensingServerParityMatterJS ports the describe
// "OccupancySensingServer" (OccupancySensingServerTest.ts:18-55).
func TestOccupancySensingServerParityMatterJS(t *testing.T) {
	t.Parallel()
	src := fakeBool{class: contract.MeasurementOccupancy, val: false, obs: true}

	// :19-23. The endpoint constructs with sensor type Pir; nothing is
	// asserted beyond it.
	t.Run("instantiates", func(t *testing.T) {
		t.Parallel()
		s := measurement.NewOccupancySensingServer(src)
		if v, ok := s.MatterRead(occdef.AttrOccupancySensorType); !ok || v != uint8(occdef.OccupancySensorTypeEnumPir) {
			t.Errorf("OccupancySensorType %v, %v, want Pir", v, ok)
		}
	})

	// :25-31.
	t.Run("chooses correct defaults", func(t *testing.T) {
		t.Parallel()
		s := measurement.NewOccupancySensingServer(src)
		if slices.Contains(s.MatterAttributes(), occdef.AttrPirUnoccupiedToOccupiedThreshold) {
			t.Error("AttributeList holds PirUnoccupiedToOccupiedThreshold without HoldTime")
		}
		if v, ok := s.MatterRead(occdef.AttrPirUnoccupiedToOccupiedThreshold); ok {
			t.Errorf("PirUnoccupiedToOccupiedThreshold reads %v, want undefined", v)
		}
	})
}
