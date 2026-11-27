// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"slices"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
)

// UnitLocalizationConfig carries the construction parameters of a
// UnitLocalization server.
type UnitLocalizationConfig struct {
	// TemperatureUnit is the unit the node starts with: the one the host
	// persisted from an earlier write, say. nil is nothing configured:
	// Celsius when SupportedTemperatureUnits holds it, else its first
	// unit. A unit SupportedTemperatureUnits does not hold is refused.
	TemperatureUnit *unitdef.TempUnitEnum
	// SupportedTemperatureUnits lists the units a controller may choose
	// (2 to 3 of them, each once); empty (nil or zero length) is
	// [Celsius, Fahrenheit].
	SupportedTemperatureUnits []unitdef.TempUnitEnum
	// OnWrite, when set, is handed a controller-written TemperatureUnit
	// that passed the checks, before it is stored; the host persists it
	// here. An error refuses the write.
	OnWrite func(ctx context.Context, unit unitdef.TempUnitEnum) error
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// NewUnitLocalization builds a UnitLocalization (0x002D) server on the
// generated definition (cluster/spec/unitlocalization, ADR 0013), with the
// TemperatureUnit feature.
//
// Mirrors matter.js packages/node/src/behaviors/unit-localization/
// UnitLocalizationServer.ts at db91886c: the behavior
// .with("TemperatureUnit") (:16); initialize (:17-38) seeds
// SupportedTemperatureUnits with [Celsius, Fahrenheit] when it is empty or
// unset (:18-23), keeps a TemperatureUnit that is set and supported
// (:24-27), and otherwise starts at Celsius when the list holds it, else
// at the list's first unit (:29-31, :37). One rule departs, the same for
// the three localization servers (decisions 289, 296): matter.js
// replaces a set unit the list lacks, with a warning (:32-36); here a
// configured TemperatureUnit is the host's explicit statement, and one
// the list lacks is refused with [ErrLocalizationConfig] — this layer
// holds no stored value of its own that could have gone stale
// (notes/parity/by_design.md
// BD-Matter-Localization-ConfiguredStartValueRefused).
//
// A written TemperatureUnit that SupportedTemperatureUnits does not hold
// is CONSTRAINT_ERROR: the model constraint is "in
// supportedTemperatureUnits" (matter.js packages/model/src/standard/
// elements/unit-localization.element.ts:24-27 at db91886c), enforced by
// matter.js's validator (packages/node/src/behavior/state/validation/
// constraint.ts:157-168 at db91886c) and by the generated runtime before
// the sink sees the value (spec.Instance.ValidateWrite).
func NewUnitLocalization(cfg UnitLocalizationConfig) (*spec.Server, error) {
	supported := slices.Clone(cfg.SupportedTemperatureUnits)
	if len(supported) == 0 {
		supported = []unitdef.TempUnitEnum{unitdef.TempUnitCelsius, unitdef.TempUnitFahrenheit}
	}
	// Each unit once: the model's text for the attribute, "Each entry in
	// the list shall be unique" (matter.js unit-localization.resource.ts:43-45
	// at db91886c); neither matter.js's constraint ("2 to 3",
	// unit-localization.element.ts:29-35 at db91886c) nor connectedhomeip
	// (UnitLocalizationCluster.cpp:52-66 at 6170af84, the size alone)
	// checks it (review II, R2-P6-err-1268).
	if sorted := slices.Sorted(slices.Values(supported)); len(slices.Compact(sorted)) != len(supported) {
		return nil, fmt.Errorf("%w: UnitLocalization: SupportedTemperatureUnits %v lists a unit twice", ErrLocalizationConfig, supported)
	}
	var unit unitdef.TempUnitEnum
	switch {
	case cfg.TemperatureUnit != nil:
		unit = *cfg.TemperatureUnit
		if !slices.Contains(supported, unit) {
			return nil, fmt.Errorf("%w: UnitLocalization: TemperatureUnit %d not in SupportedTemperatureUnits %v", ErrLocalizationConfig, unit, supported)
		}
	case slices.Contains(supported, unitdef.TempUnitCelsius):
		unit = unitdef.TempUnitCelsius
	default:
		unit = supported[0]
	}
	srv, err := spec.NewServer(unitdef.Definition, spec.Options{Features: uint32(unitdef.FeatureTemperatureUnit)}, spec.ServerConfig{
		DataVersion: cfg.DataVersion, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
		Sink:        unitSink{onWrite: cfg.OnWrite},
		Initial: map[uint32]any{
			unitdef.AttrTemperatureUnit:           unit,
			unitdef.AttrSupportedTemperatureUnits: enumList[unitdef.TempUnitEnum](supported),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: UnitLocalization: %w", err)
	}
	return srv, nil
}

// unitSink hands a written TemperatureUnit to the host. Membership in
// SupportedTemperatureUnits is the runtime's "in" constraint, refused
// before the sink is called.
type unitSink struct {
	onWrite func(ctx context.Context, unit unitdef.TempUnitEnum) error
}

// MatterWriteAttribute implements [spec.Sink]; TemperatureUnit, stored as
// a uint8, is the only writable attribute.
func (s unitSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	v, _ := value.(uint8)
	if s.onWrite != nil {
		if err := s.onWrite(ctx, unitdef.TempUnitEnum(v)); err != nil {
			return fmt.Errorf("core: UnitLocalization: %w", err)
		}
	}
	return nil
}
