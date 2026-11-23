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

	// 200 fits the config field and the server refuses it (chip's limit is
	// 10). A value the field cannot hold is left to the declaration's
	// Initial instead (R2-C2-api-1351, TestConfigFromLeavesAValueItsFieldCannotHold).
	sdl := uint8(200)
	b := defaultBuild{
		def: acsdef.Definition, initial: map[uint32]any{acsdef.AttrSimultaneousDetectionLimit: sdl},
	}
	b.opts.Features = uint32(sensing.FeatureHumanActivity)
	if _, err := newAmbientContextServer(b); err == nil || errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("AmbientContextSensing SimultaneousDetectionLimit 200: %v, want a configuration error", err)
	}
	b = defaultBuild{def: acsdef.Definition, initial: map[uint32]any{}}
	if _, err := newAmbientContextServer(b); !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("AmbientContextSensing without HA, OC, OI, AUD: %v, want ErrSelectionNotServed", err)
	}
}

// TestConfigFromLeavesAValueItsFieldCannotHold (R2-C2-api-1351): a host
// Initial out of range for its configuration field is left alone, as
// configFrom's doc says — not stored wrapped and consumed. Before,
// SimultaneousDetectionLimit 0xFFFF became 255 in a uint8 field and was
// deleted from Initial.
func TestConfigFromLeavesAValueItsFieldCannotHold(t *testing.T) {
	t.Parallel()
	var cfg struct{ SimultaneousDetectionLimit uint8 }
	initial := map[uint32]any{acsdef.AttrSimultaneousDetectionLimit: uint16(0xFFFF)}
	configFrom(&cfg, acsdef.Definition, initial)
	if cfg.SimultaneousDetectionLimit != 0 {
		t.Errorf("field = %d, want 0 (left alone)", cfg.SimultaneousDetectionLimit)
	}
	if _, ok := initial[acsdef.AttrSimultaneousDetectionLimit]; !ok {
		t.Error("the value its field cannot hold was consumed from Initial")
	}
	initial[acsdef.AttrSimultaneousDetectionLimit] = uint16(7)
	configFrom(&cfg, acsdef.Definition, initial)
	if _, ok := initial[acsdef.AttrSimultaneousDetectionLimit]; cfg.SimultaneousDetectionLimit != 7 || ok {
		t.Errorf("a value that fits: field %d, consumed %v; want 7, true", cfg.SimultaneousDetectionLimit, !ok)
	}
}
