// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	unitdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	"github.com/SukramJ/go-fabric/im"
)

// UnitLocalizationConfig carries the construction parameters of a
// UnitLocalization server.
type UnitLocalizationConfig struct {
	// TemperatureUnit is the unit the node starts with; nil is Celsius.
	TemperatureUnit *unitdef.TempUnitEnum
	// SupportedTemperatureUnits lists the units a controller may choose
	// (2 to 3 of them); empty is [Celsius, Fahrenheit]. It must hold
	// TemperatureUnit.
	SupportedTemperatureUnits []unitdef.TempUnitEnum
	// OnWrite, when set, is handed a controller-written TemperatureUnit
	// that passed the checks, before it is stored; the host persists it
	// here. An error refuses the write.
	OnWrite func(ctx context.Context, unit unitdef.TempUnitEnum) error
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewUnitLocalization builds a UnitLocalization (0x002D) server on the
// generated definition (cluster/spec/unitlocalization, ADR 0013), with the
// TemperatureUnit feature.
//
// Mirrors matter.js packages/node/src/behaviors/unit-localization/
// UnitLocalizationServer.ts: the behavior .with("TemperatureUnit") (:14);
// initialize (:15-25) starts TemperatureUnit at Celsius when unset and
// SupportedTemperatureUnits at [Celsius, Fahrenheit] when empty.
//
// A written TemperatureUnit that SupportedTemperatureUnits does not hold
// is CONSTRAINT_ERROR, as connectedhomeip answers it
// (src/app/clusters/unit-localization-server/UnitLocalizationCluster.cpp
// SetTemperatureUnit :105-115, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20). matter.js does not check it:
// the attribute's model states no "in" constraint.
func NewUnitLocalization(cfg UnitLocalizationConfig) (*spec.Server, error) {
	unit := unitdef.TempUnitCelsius
	if cfg.TemperatureUnit != nil {
		unit = *cfg.TemperatureUnit
	}
	supported := slices.Clone(cfg.SupportedTemperatureUnits)
	if len(supported) == 0 {
		supported = []unitdef.TempUnitEnum{unitdef.TempUnitCelsius, unitdef.TempUnitFahrenheit}
	}
	if !slices.Contains(supported, unit) {
		return nil, fmt.Errorf("%w: UnitLocalization: TemperatureUnit %d not in SupportedTemperatureUnits %v", ErrLocalizationConfig, unit, supported)
	}
	srv, err := spec.NewServer(unitdef.Definition, spec.Options{Features: uint32(unitdef.FeatureTemperatureUnit)}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        unitSink{supported: supported, onWrite: cfg.OnWrite},
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

// unitSink holds a written TemperatureUnit to the fixed
// SupportedTemperatureUnits and hands it to the host.
type unitSink struct {
	supported []unitdef.TempUnitEnum
	onWrite   func(ctx context.Context, unit unitdef.TempUnitEnum) error
}

// MatterWriteAttribute implements [spec.Sink]; TemperatureUnit, stored as
// a uint8, is the only writable attribute.
func (s unitSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	v, _ := value.(uint8)
	unit := unitdef.TempUnitEnum(v)
	if !slices.Contains(s.supported, unit) {
		return spec.Errorf(im.StatusConstraintError, "UnitLocalization: TemperatureUnit %d is not in SupportedTemperatureUnits", v)
	}
	if s.onWrite != nil {
		if err := s.onWrite(ctx, unit); err != nil {
			return fmt.Errorf("core: UnitLocalization: %w", err)
		}
	}
	return nil
}
