// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	booleanstatedef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	"thingfabric.dev/go-fabric/device"
)

// TestDeclareSelectsNoFeatureParityMatterJS ports "drops all features
// when invoked with no features" (matter.js
// packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts:56-62
// at db021da0).
//
// In Go: a typed Declare with NoFeatures selects none, also for
// BooleanState, whose default selection enables ChangeEvent; NoFeatures
// beside a non-zero Features is refused at New (R2-P5-apidev-04,
// N4-dec2b-2).
func TestDeclareSelectsNoFeatureParityMatterJS(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: device.StringKey(key), Name: key} }
	d, err := device.NewContactSensor(opts("default"))
	if err != nil {
		t.Fatal(err)
	}
	if fm, _ := d.BooleanState().Underlying().MatterRead(spec.AttrFeatureMap); fm != uint32(booleanstatedef.FeatureChangeEvent) {
		t.Fatalf("default FeatureMap %v, want ChangeEvent: the case needs a default-enabled feature", fm)
	}
	d, err = device.NewContactSensor(opts("none"), booleanstatedef.Declare{NoFeatures: true})
	if err != nil {
		t.Fatal(err)
	}
	if fm, _ := d.BooleanState().Underlying().MatterRead(spec.AttrFeatureMap); fm != uint32(0) {
		t.Errorf("NoFeatures: FeatureMap %v, want 0", fm)
	}
	_, err = device.NewContactSensor(opts("both"), booleanstatedef.Declare{NoFeatures: true, Features: booleanstatedef.FeatureChangeEvent})
	if !errors.Is(err, device.ErrConfiguration) {
		t.Errorf("NoFeatures with Features: %v, want ErrConfiguration", err)
	}
}
