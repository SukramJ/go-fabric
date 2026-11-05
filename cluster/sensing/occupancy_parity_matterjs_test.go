// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/occupancy-sensing/OccupancySensingServerTest.ts
// at 862ad75f on the device kit's server ([sensing.Occupancy]); the
// measurement-projected server's port is
// cluster/measurement:TestOccupancySensingServerParityMatterJS.
package sensing_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
)

// TestOccupancySensingServerParityMatterJS ports the describe
// "OccupancySensingServer" (OccupancySensingServerTest.ts:18-55).
func TestOccupancySensingServerParityMatterJS(t *testing.T) {
	// :19-23 on OccupancySensingServer.with("Radar") with the type Pir set.
	t.Run("instantiates", func(t *testing.T) {
		o, _ := newOcc(t, sensing.OccupancyConfig{Features: occdef.FeatureRadar, Clock: newFake()})
		pir := occdef.OccupancySensorTypeEnumPir
		if err := occdef.Wrap(o).Set(occdef.Patch{OccupancySensorType: &pir}); err != nil {
			t.Fatal(err)
		}
	})

	// :25-31: without HoldTime its dependents are not served.
	t.Run("chooses correct defaults", func(t *testing.T) {
		o, _ := newOcc(t, sensing.OccupancyConfig{Features: occdef.FeatureRadar, Clock: newFake()})
		if slices.Contains(o.MatterAttributes(), occdef.AttrPirUnoccupiedToOccupiedThreshold) {
			t.Error("AttributeList holds PirUnoccupiedToOccupiedThreshold without HoldTime")
		}
		if v, ok := o.MatterRead(occdef.AttrPirUnoccupiedToOccupiedThreshold); ok && v != nil {
			t.Errorf("PirUnoccupiedToOccupiedThreshold reads %v, want undefined", v)
		}
	})

	// :33-54 on PirOccupancySensing (:15): the host's change of Occupancy
	// emits one OccupancyChanged carrying the new bitmap.
	t.Run("emits OccupancyChanged when occupancy changes and the OccupancyEvent feature is enabled", func(t *testing.T) {
		o, em := newOcc(t, sensing.OccupancyConfig{Features: occdef.FeaturePassiveInfrared | occdef.FeatureOccupancyEvent, Clock: newFake()})
		if !slices.Contains(o.MatterEvents(), occdef.EventOccupancyChanged) {
			t.Fatalf("EventList %v lacks OccupancyChanged", o.MatterEvents())
		}
		setOccupied(t, o, true)
		if got := occupancyEvents(em); !slices.Equal(got, []occdef.OccupancyBitmap{occdef.OccupancyOccupied}) {
			t.Errorf("emitted %v, want [occupied]", got)
		}
	})
}
