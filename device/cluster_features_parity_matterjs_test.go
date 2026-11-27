// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts
// at db91886c, its "published behaviors" cases (:194-308) on the device
// kit: which features a cluster's default server serves, which elements a
// selection leaves out, and that a selection breaking the FeatureMap
// conformance is refused when the device is built. The with()/for()/
// alter()/enable() cases (:92-192) are TypeScript's behavior-type algebra,
// which the Go kit expresses as a declaration's Features. "Enable only
// features matter.js implements unconditionally" (:310-340) is ported as
// two tests: node's TestIntentionallyEnabledFeaturesParityMatterJS holds
// the nine INTENTIONALLY_ENABLED rows (:28-38) against the default
// servers, and the device package's TestBareDevicesServeTheDefaultSelection
// holds every generated type built bare to its default selection.
package device_test

import (
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	booleanstatedef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	levelcontroldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	powersourcedef "thingfabric.dev/go-fabric/cluster/spec/powersource"
	thermostatdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/surface"
)

func TestClusterBehaviorFeaturesParityMatterJS(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: device.StringKey(key), Name: key} }

	// "LevelControl enables nothing, though the spec assigns OnOff a
	// fallback of 1" (:195-201) and "do not serve Lighting elements on an
	// endpoint" (:235-248): a Speaker's LevelControl — a type that selects
	// no LevelControl feature — serves CurrentLevel and none of the
	// Lighting attributes.
	t.Run("LevelControl selects nothing and serves no Lighting element", func(t *testing.T) {
		t.Parallel()
		d, err := device.NewSpeaker(opts("speaker"))
		if err != nil {
			t.Fatal(err)
		}
		srv := d.LevelControl()
		fm, _ := srv.Underlying().MatterRead(spec.AttrFeatureMap)
		if fm != uint32(0) {
			t.Errorf("FeatureMap %v, want 0", fm)
		}
		if _, ok := srv.Underlying().MatterRead(levelcontroldef.AttrCurrentLevel); !ok {
			t.Error("CurrentLevel not served")
		}
		for _, id := range []uint32{levelcontroldef.AttrRemainingTime, levelcontroldef.AttrStartUpCurrentLevel} {
			if _, ok := srv.Underlying().MatterRead(id); ok {
				t.Errorf("Lighting attribute 0x%04X served without Lighting", id)
			}
		}
	})

	// "are rejected on an endpoint when the selection violates FeatureMap
	// conformance" (:250-275): PowerSource needs one of Wired or Battery and
	// takes not both; Wired alone builds.
	t.Run("a selection breaking the FeatureMap conformance is refused", func(t *testing.T) {
		t.Parallel()
		for name, f := range map[string]powersourcedef.Feature{
			"none": 0,
			"both": powersourcedef.FeatureWired | powersourcedef.FeatureBattery,
		} {
			_, err := device.NewPowerSource(opts("ps-"+name), powersourcedef.Declare{Features: f})
			if !errors.Is(err, device.ErrConfiguration) {
				t.Errorf("%s: %v, want ErrConfiguration", name, err)
			}
		}
		if _, err := device.NewPowerSource(opts("ps-wired"), powersourcedef.Declare{
			Features: powersourcedef.FeatureWired, Initial: powersourcedef.Patch{Order: new(uint8(1))},
		}); err != nil {
			t.Errorf("Wired: %v", err)
		}
	})

	// "enable only features matter.js implements unconditionally"
	// (:310-340) with its INTENTIONALLY_ENABLED table (:28-38):
	// BooleanStateServer enables ChangeEvent (:30;
	// packages/node/src/behaviors/boolean-state/BooleanStateServer.ts:11-13
	// at db021da0), so a device that declares nothing about BooleanState
	// serves the feature and lists StateChange.
	t.Run("BooleanState serves ChangeEvent by default", func(t *testing.T) {
		t.Parallel()
		d, err := device.NewContactSensor(opts("contact"))
		if err != nil {
			t.Fatal(err)
		}
		srv := d.BooleanState().Underlying()
		fm, _ := srv.MatterRead(spec.AttrFeatureMap)
		if fm != uint32(booleanstatedef.FeatureChangeEvent) {
			t.Errorf("FeatureMap %v, want ChangeEvent (%d)", fm, booleanstatedef.FeatureChangeEvent)
		}
		lister, ok := srv.(interface{ MatterEvents() []uint32 })
		if !ok || !slices.Contains(lister.MatterEvents(), booleanstatedef.EventStateChange) {
			t.Error("StateChange is not among the server's events")
		}
	})

	// "publish nothing of what their base implementation enables"
	// (:203-229): a Thermostat device selects only what its device type
	// mandates — Heating or Cooling is the type's choice, not all of the
	// base server's AutoMode, Occupancy and Presets.
	t.Run("a default selects no base-implementation feature", func(t *testing.T) {
		t.Parallel()
		d, err := device.NewThermostat(opts("thermo"), thermostatdef.Declare{Features: thermostatdef.FeatureHeating})
		if err != nil {
			t.Fatal(err)
		}
		fm, _ := d.Thermostat().Underlying().MatterRead(spec.AttrFeatureMap)
		if fm != uint32(thermostatdef.FeatureHeating) {
			t.Errorf("FeatureMap %v, want Heating alone: the kit adds none of the base server's features", fm)
		}
	})

	// "drops the default the schema selected" (:123-126, #5057):
	// ThermostatRunningMode ("TEVT & AUTO, [AUTO]") is served, Off, with
	// AutoMode and Events; with AutoMode alone it is optional and not
	// served — a Go selection has no derivation chain, so no default of a
	// wider selection can carry over.
	t.Run("ThermostatRunningMode is served with AutoMode and Events only", func(t *testing.T) {
		t.Parallel()
		base := thermostatdef.FeatureHeating | thermostatdef.FeatureCooling | thermostatdef.FeatureAutoMode
		for _, tc := range []struct {
			name     string
			features thermostatdef.Feature
			served   bool
		}{
			{"AutoMode and Events", base | thermostatdef.FeatureEvents, true},
			{"AutoMode without Events", base, false},
		} {
			p := surface.Policies{Extensions: surface.NewExtensions("BD-chip-Thermostat-Events")}
			o := opts("thermo")
			o.Surface = p
			d, err := device.NewThermostat(o, thermostatdef.Declare{Features: tc.features})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			srv := d.Thermostat().Underlying()
			lister, ok := srv.(interface{ MatterAttributes() []uint32 })
			if !ok {
				t.Fatalf("%s: the server lists no attributes", tc.name)
			}
			listed := slices.Contains(lister.MatterAttributes(), thermostatdef.AttrThermostatRunningMode)
			v, held := srv.MatterRead(thermostatdef.AttrThermostatRunningMode)
			switch {
			case listed != tc.served || held != tc.served:
				t.Errorf("%s: ThermostatRunningMode listed %v, read %v; want served %v", tc.name, listed, held, tc.served)
			case tc.served && v != uint8(thermostatdef.ThermostatRunningModeOff):
				t.Errorf("%s: ThermostatRunningMode %v, want Off", tc.name, v)
			}
		}
	})
}
