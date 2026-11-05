// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/occupancy-sensing/OccupancySensingServerTest.ts
// at 862ad75f on the measurement-projected server; the emission of
// OccupancyChanged is bridge:TestOccupancyChangedEvent here and
// cluster/sensing:TestOccupancySensingServerParityMatterJS on the device
// kit's server.
//
// matter.js builds OccupancySensingServer.with("Radar") for the first two
// cases; the projected server takes the source's detector selection
// (contract.OccupancySelection), so they run on Radar. The defaults case
// asserts the HoldTime-dependent PirUnoccupiedToOccupiedThreshold absent:
// matter.js clears every HoldTime dependent when HoldTime is unset
// (OccupancySensingServer.ts initialize, holdTimeDependencies), and the
// projected server serves no HoldTime and none of its dependents. The
// third case's PirOccupancySensing is the projection's default selection,
// PassiveInfrared with OccupancyEvent (:15).
package measurement_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
)

// TestOccupancySensingServerParityMatterJS ports the describe
// "OccupancySensingServer" (OccupancySensingServerTest.ts:18-55).
func TestOccupancySensingServerParityMatterJS(t *testing.T) {
	t.Parallel()
	src := radarOccupancy{fakeBool{class: contract.MeasurementOccupancy, val: false, obs: true}}

	// :19-23. The endpoint constructs with sensor type Pir (the type a
	// Radar selection leaves at its default); nothing is asserted beyond
	// it.
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

	// :33-54: the default selection is PassiveInfrared with OccupancyEvent,
	// so the server advertises the feature and lists the event the bridge
	// emits on each change.
	t.Run("emits OccupancyChanged when occupancy changes and the OccupancyEvent feature is enabled", func(t *testing.T) {
		t.Parallel()
		s := measurement.NewOccupancySensingServer(fakeBool{class: contract.MeasurementOccupancy, val: false, obs: true})
		fm, _ := s.MatterRead(cluster.AttrGlobalFeatureMap)
		if fm != uint32(occdef.FeaturePassiveInfrared|occdef.FeatureOccupancyEvent) {
			t.Errorf("FeatureMap %v, want PassiveInfrared|OccupancyEvent", fm)
		}
		if !slices.Contains(s.MatterEvents(), occdef.EventOccupancyChanged) {
			t.Errorf("EventList %v lacks OccupancyChanged", s.MatterEvents())
		}
	})
}

// radarOccupancy is an occupancy reading of a Radar sensor.
type radarOccupancy struct{ fakeBool }

func (radarOccupancy) MatterOccupancySelection() contract.OccupancySelection {
	return contract.OccupancySelection{Detectors: contract.OccupancyDetectorRadar}
}
