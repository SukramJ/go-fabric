// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts
// at 862ad75f, its "published behaviors" cases (:121-200) on the device
// kit: which features a cluster's default server serves, which elements a
// selection leaves out, and that a selection breaking the FeatureMap
// conformance is refused when the device is built. The with()/for()/
// alter()/enable() cases (:47-119) are TypeScript's behavior-type algebra,
// which the Go kit expresses as a declaration's Features; "enable only
// features matter.js implements unconditionally" (:201-232) walks
// matter.js's exported behavior classes and has no Go counterpart beyond
// the cases below.
package device_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
)

func TestClusterBehaviorFeaturesParityMatterJS(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: device.StringKey(key), Name: key} }

	// "LevelControl enables nothing, though the spec assigns OnOff a
	// fallback of 1" (:122-128) and "do not serve Lighting elements on an
	// endpoint" (:159-172): a Speaker's LevelControl — a type that selects
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
	// conformance" (:174-198): PowerSource needs one of Wired or Battery and
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

	// "publish nothing of what their base implementation enables"
	// (:130-153): a Thermostat device selects only what its device type
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
}
