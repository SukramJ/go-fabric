// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"fmt"
	"strconv"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	unitdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	"github.com/SukramJ/go-fabric/im"
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
//   - KeypadLockout is NoLockout unless the state holds one (:30-32): the
//     configured value ([ThermostatUIConfig.KeypadLockout]), or — with a
//     [SettingStore] — the one written before a restart, which persists
//     as matter.js persists the attribute (quality N,
//     thermostat-user-interface-configuration.element.ts).

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
	// KeypadLockout is the initial KeypadLockout; nil is NoLockout.
	KeypadLockout *KeypadLockout
	// Settings, optional, persists KeypadLockout: a value stored there
	// replaces KeypadLockout at construction, and every change — a
	// controller's write or the device's own ([ThermostatUIServer.Set]) —
	// is saved; nil keeps it in memory. Every store.Port is one.
	Settings SettingStore
	// SettingKey is the key KeypadLockout persists under in Settings;
	// empty is [DefaultKeypadLockoutSetting]. An endpoint of its own needs
	// a key of its own.
	SettingKey string
	// Sink, optional, is told about every controller write the
	// definition admits, before it is stored (the host persists the
	// setting); an error refuses the write.
	Sink spec.Sink
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// SettingStore is the settings table KeypadLockout persists in:
// store.Store's GetSetting / SetSetting.
type SettingStore = tsuic.SettingStore

// DefaultKeypadLockoutSetting is the settings key KeypadLockout persists
// under when [ThermostatUIConfig.SettingKey] is empty.
const DefaultKeypadLockoutSetting = "thermostat_user_interface_configuration.keypad_lockout"

// ThermostatUIServer is the ThermostatUserInterfaceConfiguration server:
// the generated default server with matter.js's initial values.
type ThermostatUIServer struct {
	*spec.Server
	settings SettingStore
	key      string
}

// persistingSink saves a controller's KeypadLockout write before the
// server stores it, after the host's Sink consented; a failed save
// refuses the write, as a failed commit refuses it in matter.js.
type persistingSink struct {
	next     spec.Sink
	settings SettingStore
	key      string
}

func (p persistingSink) MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error {
	if p.next != nil {
		if err := p.next.MatterWriteAttribute(ctx, attrID, value); err != nil {
			return err
		}
	}
	if attrID != tsuic.AttrKeypadLockout || p.settings == nil {
		return nil
	}
	if err := p.settings.SetSetting(ctx, p.key, fmt.Sprintf("%d", value)); err != nil {
		return spec.Errorf(im.StatusFailure, "thermostatuserinterfaceconfiguration: save KeypadLockout: %v", err)
	}
	return nil
}

// NewThermostatUserInterfaceConfiguration builds the server. Its initial
// state is matter.js's initialize: TemperatureDisplayMode as configured,
// else from UnitLocalization, else Celsius; KeypadLockout as stored in
// Settings, else as configured, else NoLockout.
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
	key := cfg.SettingKey
	if key == "" {
		key = DefaultKeypadLockoutSetting
	}
	keypad := tsuic.KeypadLockoutNoLockout
	if cfg.KeypadLockout != nil {
		keypad = *cfg.KeypadLockout
	}
	if cfg.Settings != nil {
		v, ok, err := cfg.Settings.GetSetting(context.Background(), key)
		if err != nil {
			return nil, fmt.Errorf("thermostatuserinterfaceconfiguration: load KeypadLockout: %w", err)
		}
		if ok {
			n, perr := strconv.ParseUint(v, 10, 8)
			if perr != nil {
				return nil, fmt.Errorf("thermostatuserinterfaceconfiguration: stored KeypadLockout %q: %w", v, perr)
			}
			keypad = KeypadLockout(n)
		}
	}
	initial := map[uint32]any{
		tsuic.AttrTemperatureDisplayMode: display,
		tsuic.AttrKeypadLockout:          keypad,
	}
	var opts spec.Options
	if cfg.ScheduleProgrammingVisibility != nil {
		opts.Attributes = []uint32{tsuic.AttrScheduleProgrammingVisibility}
		initial[tsuic.AttrScheduleProgrammingVisibility] = *cfg.ScheduleProgrammingVisibility
	}
	var sink spec.Sink = persistingSink{next: cfg.Sink, settings: cfg.Settings, key: key}
	if cfg.Settings == nil {
		sink = cfg.Sink
	}
	srv, err := spec.NewServer(tsuic.Definition, opts, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        sink,
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("thermostatuserinterfaceconfiguration: %w", err)
	}
	return &ThermostatUIServer{Server: srv, settings: cfg.Settings, key: key}, nil
}

// Set records a change the device made to attrID, as [spec.Server.Set]
// does; a KeypadLockout it changes is saved to the Settings.
func (s *ThermostatUIServer) Set(attrID uint32, value any) error {
	return s.SetAttributes(map[uint32]any{attrID: value})
}

// SetAttributes is [spec.Server.SetAttributes]; a KeypadLockout it changes
// is saved to the Settings.
func (s *ThermostatUIServer) SetAttributes(values map[uint32]any) error {
	if err := s.Server.SetAttributes(values); err != nil {
		return err
	}
	if _, ok := values[tsuic.AttrKeypadLockout]; !ok || s.settings == nil {
		return nil
	}
	v, _ := s.Value(tsuic.AttrKeypadLockout)
	if err := s.settings.SetSetting(context.Background(), s.key, fmt.Sprintf("%d", v)); err != nil {
		return fmt.Errorf("thermostatuserinterfaceconfiguration: save KeypadLockout: %w", err)
	}
	return nil
}
