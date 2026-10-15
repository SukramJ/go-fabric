// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	unitdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
)

// ThermostatUserInterfaceConfiguration (0x0204) is built on its generated
// definition (cluster/spec/thermostatuserinterfaceconfiguration; ADR
// 0013) and served by the generated default server ([spec.Server], ADR
// 0017): the ids, the enums, the attribute list, ClusterRevision, the
// privileges (TemperatureDisplayMode RW VO, KeypadLockout and
// ScheduleProgrammingVisibility RW VM) and the write answers all come
// from the definition. The one rule matter.js adds is its initialize
// (packages/node/src/behaviors/thermostat-user-interface-configuration/
// ThermostatUserInterfaceConfigurationServer.ts):
//
//   - TemperatureDisplayMode, when the host gives none, is Fahrenheit if
//     the same endpoint's UnitLocalization server reads TemperatureUnit
//     Fahrenheit, else Celsius (:16-28: `this.agent.has(
//     UnitLocalizationServer)` asks the TSUIC's own endpoint). The host
//     hands that server in as [ThermostatUIConfig.UnitLocalization]; it is
//     read once, at construction, as matter.js reads it once at initialize.
//   - KeypadLockout is NoLockout on every start, whatever was stored
//     before (:30).

// ClusterIDThermostatUserInterfaceConfiguration is the cluster id, from the
// generated definition.
const ClusterIDThermostatUserInterfaceConfiguration = tsuic.ClusterID

// The ThermostatUserInterfaceConfiguration enums, from the generated
// definition.
type (
	// TemperatureDisplayMode is the TemperatureDisplayModeEnum.
	TemperatureDisplayMode = tsuic.TemperatureDisplayModeEnum
	// KeypadLockout is the KeypadLockoutEnum.
	KeypadLockout = tsuic.KeypadLockoutEnum
	// ScheduleProgrammingVisibility is the
	// ScheduleProgrammingVisibilityEnum.
	ScheduleProgrammingVisibility = tsuic.ScheduleProgrammingVisibilityEnum
)

// TemperatureDisplayMode values.
const (
	TemperatureDisplayCelsius    = tsuic.TemperatureDisplayModeCelsius
	TemperatureDisplayFahrenheit = tsuic.TemperatureDisplayModeFahrenheit
)

// ThermostatUIConfig carries the construction parameters.
type ThermostatUIConfig struct {
	// TemperatureDisplayMode is the initial display unit; nil takes it
	// from UnitLocalization, else Celsius, matter.js's fallback.
	TemperatureDisplayMode *TemperatureDisplayMode
	// UnitLocalization, optional, is the UnitLocalization (0x002D) server
	// of the same endpoint (core.NewUnitLocalization). When
	// TemperatureDisplayMode is nil and its TemperatureUnit reads
	// Fahrenheit at construction, the display mode starts at Fahrenheit.
	// Mirrors matter.js
	// ThermostatUserInterfaceConfigurationServer.ts:18-27.
	UnitLocalization *spec.Server
	// ScheduleProgrammingVisibility, when set, serves the optional
	// attribute with this initial value; nil leaves it out.
	ScheduleProgrammingVisibility *ScheduleProgrammingVisibility
	// Sink, optional, is told about every controller write the
	// definition admits, before it is stored (the host persists the
	// setting); an error refuses the write.
	Sink spec.Sink
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// ThermostatUIServer is the ThermostatUserInterfaceConfiguration server:
// the generated default server with matter.js's initial values.
type ThermostatUIServer struct {
	*spec.Server
}

// NewThermostatUserInterfaceConfiguration builds the server. Its initial
// state is matter.js's initialize: TemperatureDisplayMode as configured,
// else from UnitLocalization, else Celsius; KeypadLockout NoLockout.
func NewThermostatUserInterfaceConfiguration(cfg ThermostatUIConfig) (*ThermostatUIServer, error) {
	display := TemperatureDisplayCelsius
	switch {
	case cfg.TemperatureDisplayMode != nil:
		display = *cfg.TemperatureDisplayMode
	case cfg.UnitLocalization != nil:
		// matter.js ThermostatUserInterfaceConfigurationServer.ts:20-24.
		if v, ok := cfg.UnitLocalization.MatterRead(unitdef.AttrTemperatureUnit); ok && v == any(uint8(unitdef.TempUnitFahrenheit)) {
			display = TemperatureDisplayFahrenheit
		}
	}
	initial := map[uint32]any{
		tsuic.AttrTemperatureDisplayMode: display,
		tsuic.AttrKeypadLockout:          tsuic.KeypadLockoutNoLockout,
	}
	var opts spec.Options
	if cfg.ScheduleProgrammingVisibility != nil {
		opts.Attributes = []uint32{tsuic.AttrScheduleProgrammingVisibility}
		initial[tsuic.AttrScheduleProgrammingVisibility] = *cfg.ScheduleProgrammingVisibility
	}
	srv, err := spec.NewServer(tsuic.Definition, opts, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        cfg.Sink,
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("thermostatuserinterfaceconfiguration: %w", err)
	}
	return &ThermostatUIServer{Server: srv}, nil
}
