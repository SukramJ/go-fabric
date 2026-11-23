// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	tsuic "thingfabric.dev/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
	"thingfabric.dev/go-fabric/im"
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
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
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
	keypad *keypadSaves
}

// keypadSaves serialises the saves of KeypadLockout: the host's save and
// store in [ThermostatUIServer.SetAttributes], a controller's save in
// persistingSink, and the check after a controller's store
// ([ThermostatUIServer.MatterWriteCommit]). saved is the value last
// written to the Settings, "" before the first.
type keypadSaves struct {
	mu       sync.Mutex
	saved    string
	settings SettingStore
	key      string
}

// save writes v to the Settings; the caller holds k.mu.
func (k *keypadSaves) save(ctx context.Context, v any) error {
	text := fmt.Sprintf("%d", v)
	if err := k.settings.SetSetting(ctx, k.key, text); err != nil {
		return err
	}
	k.saved = text
	return nil
}

// persistingSink saves a controller's KeypadLockout write before the
// server stores it, after the host's Sink consented; a failed save
// refuses the write, as a failed commit refuses it in matter.js.
type persistingSink struct {
	next   spec.Sink
	keypad *keypadSaves
}

func (p persistingSink) MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error {
	if p.next != nil {
		if err := p.next.MatterWriteAttribute(ctx, attrID, value); err != nil {
			return err
		}
	}
	if attrID != tsuic.AttrKeypadLockout || p.keypad == nil {
		return nil
	}
	p.keypad.mu.Lock()
	defer p.keypad.mu.Unlock()
	if err := p.keypad.save(ctx, value); err != nil {
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
	var keypadSaver *keypadSaves
	var sink spec.Sink = cfg.Sink
	if cfg.Settings != nil {
		keypadSaver = &keypadSaves{settings: cfg.Settings, key: key}
		sink = persistingSink{next: cfg.Sink, keypad: keypadSaver}
	}
	srv, err := spec.NewServer(tsuic.Definition, opts, spec.ServerConfig{
		DataVersion: cfg.DataVersion, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
		Sink:        sink,
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("thermostatuserinterfaceconfiguration: %w", err)
	}
	return &ThermostatUIServer{Server: srv, keypad: keypadSaver}, nil
}

// Set records a change the device made to attrID, as [spec.Server.Set]
// does; a KeypadLockout it changes is saved to the Settings.
func (s *ThermostatUIServer) Set(attrID uint32, value any) error {
	return s.SetAttributes(map[uint32]any{attrID: value})
}

// SetAttributes is [spec.Server.SetAttributes]; a KeypadLockout it sets
// is saved to the Settings first, as a controller's write is
// (persistingSink): a failed save refuses the call and nothing is stored
// (R2-P6-store-1563). When the store then refuses another value of the
// call, the saved KeypadLockout is put back. The save and the store are
// one step against a controller's write of KeypadLockout, so the
// Settings and the served value do not part (R2-C2-store-1555).
func (s *ThermostatUIServer) SetAttributes(values map[uint32]any) error {
	given, ok := values[tsuic.AttrKeypadLockout]
	if !ok || s.keypad == nil {
		return s.Server.SetAttributes(values)
	}
	v, err := s.CheckSet(tsuic.AttrKeypadLockout, given, s.MatterRead)
	if err != nil {
		return fmt.Errorf("thermostatuserinterfaceconfiguration: %w", err)
	}
	s.keypad.mu.Lock()
	defer s.keypad.mu.Unlock()
	old, _ := s.Value(tsuic.AttrKeypadLockout)
	ctx := context.Background()
	if err := s.keypad.save(ctx, v); err != nil {
		return fmt.Errorf("thermostatuserinterfaceconfiguration: save KeypadLockout: %w", err)
	}
	if err := s.Server.SetAttributes(values); err != nil {
		if rerr := s.keypad.save(ctx, old); rerr != nil {
			return errors.Join(err, fmt.Errorf("thermostatuserinterfaceconfiguration: restore KeypadLockout: %w", rerr))
		}
		return err
	}
	return nil
}

// MatterWrite is [spec.Server.MatterWrite] through
// [ThermostatUIServer.MatterWriteCommit].
func (s *ThermostatUIServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := s.MatterWriteCommit(ctx, attrID, value)
	return err
}

// MatterWriteChanged is [spec.Server.MatterWriteChanged] through
// [ThermostatUIServer.MatterWriteCommit].
func (s *ThermostatUIServer) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	changed, err := s.MatterWriteCommit(ctx, attrID, value)
	return len(changed) > 0, err
}

// MatterWriteCommit is [spec.Server.MatterWriteCommit]; after a write
// changed KeypadLockout, the Settings are brought to
// the served value when a host's Set saved and stored between the
// write's save and its store (R2-C2-store-1555). A failed save there is
// logged; the next save of the setting corrects it.
func (s *ThermostatUIServer) MatterWriteCommit(ctx context.Context, attrID uint32, value any) ([]uint32, error) {
	changed, err := s.Server.MatterWriteCommit(ctx, attrID, value)
	if err != nil || s.keypad == nil || !slices.Contains(changed, tsuic.AttrKeypadLockout) {
		return changed, err
	}
	s.keypad.mu.Lock()
	defer s.keypad.mu.Unlock()
	cur, _ := s.Value(tsuic.AttrKeypadLockout)
	if fmt.Sprintf("%d", cur) == s.keypad.saved {
		return changed, nil
	}
	if serr := s.keypad.save(ctx, cur); serr != nil {
		slog.Default().Warn("matter.thermostat_ui.keypad_lockout_not_saved", slog.String("reason", serr.Error()))
	}
	return changed, nil
}
