// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/sensing"
	acsdef "thingfabric.dev/go-fabric/cluster/spec/ambientcontextsensing"
	occdef "thingfabric.dev/go-fabric/cluster/spec/occupancysensing"
	"thingfabric.dev/go-fabric/endpoint"
)

// TestSensingValueErrorIsAConfigurationError (R2-P6-err-2516): a value
// the kit's sensing server refuses under a selection it serves is the
// host's configuration error, not "the selection is not served".
func TestSensingValueErrorIsAConfigurationError(t *testing.T) {
	t.Parallel()
	hold := uint16(5)
	limits := occdef.HoldTimeLimitsStruct{HoldTimeMin: 10, HoldTimeMax: 100, HoldTimeDefault: 20}
	_, err := New(OccupancySensorType(), Options{StableKey: endpoint.StringKey("occ")}, occdef.Declare{
		Features: occdef.FeaturePassiveInfrared,
		Initial:  occdef.Patch{HoldTime: &hold, HoldTimeLimits: &limits},
	})
	if !errors.Is(err, ErrConfiguration) || errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("OccupancySensing HoldTime outside its limits: %v, want ErrConfiguration and not ErrSelectionNotServed", err)
	}

	sdl := uint16(0xFFFF)
	b := defaultBuild{
		def: acsdef.Definition, initial: map[uint32]any{acsdef.AttrSimultaneousDetectionLimit: sdl},
	}
	b.opts.Features = uint32(sensing.FeatureHumanActivity)
	if _, err := newAmbientContextServer(b); err == nil || errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("AmbientContextSensing SimultaneousDetectionLimit 0xFFFF: %v, want a configuration error", err)
	}
	b = defaultBuild{def: acsdef.Definition, initial: map[uint32]any{}}
	if _, err := newAmbientContextServer(b); !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("AmbientContextSensing without HA, OC, OI, AUD: %v, want ErrSelectionNotServed", err)
	}
}
