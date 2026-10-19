// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// TestPresetWriteRules holds the remaining rules of matter.js
// #validatePresetWriteRequest (ThermostatServer.ts:1356-1484) and
// #normalizeAndValidatePresetCommit (:1508-1595) to their statuses and
// texts, on local writes.
func TestPresetWriteRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	builtIn := newPreset(withHandle([]byte{7}), func(p *thermo.Preset) { p.BuiltIn = spec.ValueOf(true) })
	cfg := thermostatConfig(5, []thermo.Preset{builtIn})
	cfg.PresetTypes = []thermo.PresetType{
		{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 5, PresetTypeFeatures: tdef.PresetTypeFeaturesSupportsNames},
		{PresetScenario: tdef.PresetScenarioSleep, NumberOfPresets: 1, PresetTypeFeatures: tdef.PresetTypeFeaturesSupportsNames},
	}
	stored := builtIn // normalisation leaves a preset with a handle and a BuiltIn as it is
	sleep := func(p *thermo.Preset) { p.PresetScenario = tdef.PresetScenarioSleep }
	noCool := func(p *thermo.Preset) { p.CoolingSetpoint = nil }
	noHeat := func(p *thermo.Preset) { p.HeatingSetpoint = nil }
	hot := func(p *thermo.Preset) { c := int16(3300); p.CoolingSetpoint = &c }
	nullBuiltIn := func(p *thermo.Preset) { p.BuiltIn = spec.NullOf[bool]() }
	for name, c := range map[string]struct {
		presets []thermo.Preset
		status  im.StatusCode
		text    string
	}{
		"a handle twice":        {[]thermo.Preset{stored, stored}, im.StatusConstraintError, "Duplicate presetHandle 07"},
		"a built-in changed":    {[]thermo.Preset{newPreset(withHandle([]byte{7}))}, im.StatusConstraintError, "Cannot change built-in status"},
		"a new built-in":        {[]thermo.Preset{stored, newPreset(func(p *thermo.Preset) { p.BuiltIn = spec.ValueOf(true) })}, im.StatusConstraintError, "Can not add a new built-in preset"},
		"a name twice":          {[]thermo.Preset{stored, newPreset(withName("a")), newPreset(withName("a"))}, im.StatusConstraintError, `Duplicate preset name "a" for scenario Occupied`},
		"a type's count":        {[]thermo.Preset{stored, newPreset(sleep), newPreset(sleep, withName("b"))}, im.StatusResourceExhausted, "Number of presets (1) for scenario Sleep exceeds allowed number (1)"},
		"no cooling setpoint":   {[]thermo.Preset{stored, newPreset(noCool, withName("c"))}, im.StatusConstraintError, "missing required coolingSetpoint"},
		"no heating setpoint":   {[]thermo.Preset{stored, newPreset(noHeat, withName("h"))}, im.StatusConstraintError, "missing required heatingSetpoint"},
		"cooling out of bounds": {[]thermo.Preset{stored, newPreset(hot, withName("t"))}, im.StatusConstraintError, "Preset coolingSetpoint (3300) for scenario Occupied is out of bounds [1600, 3200]"},
		"the built-in removed":  {[]thermo.Preset{newPreset()}, im.StatusConstraintError, "Cannot remove built-in preset with presetHandle 07"},
		"an undefined scenario": {[]thermo.Preset{stored, newPreset(func(p *thermo.Preset) { p.PresetScenario = 9 })}, im.StatusConstraintError, "No PresetType defined for scenario 9"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantStatusCode(t, mustNew(t, cfg).SetPresets(ctx, c.presets), c.status, c.text)
		})
	}
	// A null BuiltIn keeps the stored preset's.
	srv := mustNew(t, cfg)
	kept := stored
	nullBuiltIn(&kept)
	if err := srv.SetPresets(ctx, []thermo.Preset{kept, newPreset(nullBuiltIn, withName("n"))}); err != nil {
		t.Fatal(err)
	}
	got := srv.Presets()
	if len(got) != 2 || !got[0].BuiltIn.Value || got[1].BuiltIn.Null || got[1].BuiltIn.Value {
		t.Errorf("presets %+v: want the stored BuiltIn kept and the new one false", got)
	}
}

// TestPresetAPIsWithoutTheirFeature: the preset and occupancy APIs of a
// server without PRES / OCC refuse or log, and the payloads they cannot
// read are INVALID_COMMAND.
func TestPresetAPIsWithoutTheirFeature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plain := mustNew(t, thermo.DefaultThermostatConfig())
	wantStatusCode(t, plain.SetPresets(ctx, nil), im.StatusUnsupportedAttribute, "")
	wantStatusCode(t, plain.SetActivePresetHandle(ctx, nil), im.StatusUnsupportedAttribute, "")
	plain.SetOccupied(false) // logged and ignored
	if _, ok := plain.MatterRead(tdef.AttrOccupancy); ok {
		t.Error("Occupancy served without OCC")
	}

	srv := mustNew(t, thermostatConfig(5, []thermo.Preset{}))
	if err := srv.SetActivePresetHandle(ctx, make([]byte, 17)); err == nil {
		t.Error("a 17-byte ActivePresetHandle accepted")
	}
	if err := srv.SetActivePresetHandle(ctx, nil); err != nil {
		t.Errorf("an unchanged null handle: %v", err)
	}
	for _, fields := range []any{nil, (*tdef.SetActivePresetRequestRequest)(nil), map[string]any{}} {
		_, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdSetActivePresetRequest, fields)
		wantStatusCode(t, err, im.StatusInvalidCommand, "")
	}
	r := &tdef.SetActivePresetRequestRequest{PresetHandle: spec.NullOf[[]byte]()}
	if _, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdSetActivePresetRequest, r); err != nil {
		t.Errorf("SetActivePresetRequest(*null): %v", err)
	}
	for _, fields := range []any{nil, (*tdef.AtomicRequestRequest)(nil)} {
		_, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdAtomicRequest, fields)
		wantStatusCode(t, err, im.StatusInvalidCommand, "")
	}
	if _, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdAtomicRequest, &tdef.AtomicRequestRequest{RequestType: rollbackWrite, AttributeRequests: []uint32{presetsAttribute}}); err == nil {
		t.Error("a RollbackWrite without a BeginWrite accepted")
	}
	if err := srv.MatterWrite(remoteCtx(1, 5), presetsAttribute, "not a list"); err == nil {
		t.Error("a Presets write of a string accepted")
	}
	if _, err := thermo.DecodePresets([]byte{0xFF}); err == nil {
		t.Error("DecodePresets of garbage succeeded")
	}
}

// TestSetAbsoluteSetpointLimit: the device's own change of an absolute
// limit is stored and announced when the user limits stay within it.
func TestSetAbsoluteSetpointLimit(t *testing.T) {
	t.Parallel()
	srv := mustNew(t, thermo.DefaultThermostatConfig())
	changes := recordThermostatChanges(srv)
	if err := srv.SetAbsoluteSetpointLimit(tdef.AttrOccupiedHeatingSetpoint, 1); err == nil {
		t.Error("a setpoint taken for an absolute limit")
	}
	for attr, v := range map[uint32]int16{
		tdef.AttrAbsMinHeatSetpointLimit: 600, tdef.AttrAbsMaxHeatSetpointLimit: 3100,
		tdef.AttrAbsMinCoolSetpointLimit: 1500, tdef.AttrAbsMaxCoolSetpointLimit: 3300,
	} {
		if err := srv.SetAbsoluteSetpointLimit(attr, v); err != nil {
			t.Fatalf("0x%04X = %d: %v", attr, v, err)
		}
		if got := readInt16(t, srv, attr); got != v {
			t.Errorf("0x%04X = %d, want %d", attr, got, v)
		}
		if changes.count(attr) != 1 {
			t.Errorf("0x%04X announced %d times, want 1", attr, changes.count(attr))
		}
	}
	wantStatusCode(t, srv.SetAbsoluteSetpointLimit(tdef.AttrAbsMaxHeatSetpointLimit, 2000), im.StatusConstraintError,
		"maxHeatSetpointLimit (3000) must be less than or equal to absMaxHeatSetpointLimit (2000)")
	heat := mustNew(t, thermo.ThermostatConfig{Features: thermo.ThermostatFeatureHEAT, AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000, InitialHeatingSetpoint: 2000})
	wantStatusCode(t, heat.SetAbsoluteSetpointLimit(tdef.AttrAbsMinCoolSetpointLimit, 1600), im.StatusUnsupportedAttribute, "")
	// min > max of the user limits cannot arise from an absolute limit
	// change alone; an absolute minimum above the maximum is refused by the
	// user-limit order.
	wantStatusCode(t, heat.SetAbsoluteSetpointLimit(tdef.AttrAbsMinHeatSetpointLimit, 900), im.StatusConstraintError,
		"absMinHeatSetpointLimit (900) must be less than or equal to minHeatSetpointLimit (700)")
}

// sinkRecorder records the Sink's setpoint calls; err refuses them.
type sinkRecorder struct {
	calls []uint32
	err   error
}

func (s *sinkRecorder) SetSystemMode(context.Context, uint8) error { return s.err }

func (s *sinkRecorder) SetSetpoint(_ context.Context, attrID uint32, _ int16, companions []thermo.SetpointChange) error {
	s.calls = append(s.calls, attrID)
	for _, c := range companions {
		s.calls = append(s.calls, c.AttrID)
	}
	return s.err
}

// TestSetActivePresetRequestGoesThroughTheSink: the preset's setpoints
// reach the Sink in one call, heating first; a refusal stores nothing and
// leaves the preset inactive.
func TestSetActivePresetRequestGoesThroughTheSink(t *testing.T) {
	t.Parallel()
	sink := &sinkRecorder{}
	cfg := thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2200, 2700)})
	cfg.Sink = sink
	srv := mustNew(t, cfg)
	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if len(sink.calls) != 2 || sink.calls[0] != tdef.AttrOccupiedHeatingSetpoint || sink.calls[1] != tdef.AttrOccupiedCoolingSetpoint {
		t.Errorf("Sink calls %v, want heating then cooling", sink.calls)
	}
	if err := activate(t, srv, nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(remoteCtx(1, 5), tdef.AttrOccupiedHeatingSetpoint, int16(2000)); err != nil {
		t.Fatal(err)
	}
	sink.err = errors.New("the device refuses")
	if err := activate(t, srv, []byte{1}); err == nil {
		t.Fatal("a refused preset activated")
	}
	if srv.ActivePresetHandle() != nil || readInt16(t, srv, tdef.AttrOccupiedHeatingSetpoint) != 2000 {
		t.Error("a refused activation changed the state")
	}
}

// memorySettings is a SettingStore in memory; fail refuses saves.
type memorySettings struct {
	values map[string]string
	fail   error
}

func (m *memorySettings) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	value, ok = m.values[key]
	return value, ok, nil
}

func (m *memorySettings) SetSetting(_ context.Context, key, value string) error {
	if m.fail != nil {
		return m.fail
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[key] = value
	return nil
}

// TestKeypadLockoutPersistsAControllersWrite: a controller's write is saved
// under the setting key before it is stored; a failed save refuses it.
func TestKeypadLockoutPersistsAControllersWrite(t *testing.T) {
	t.Parallel()
	settings := &memorySettings{}
	srv, err := thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{Settings: settings, SettingKey: "ui.1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), tsuic.AttrKeypadLockout, uint8(3)); err != nil {
		t.Fatal(err)
	}
	if settings.values["ui.1"] != "3" {
		t.Errorf("saved %q, want 3", settings.values["ui.1"])
	}
	settings.fail = errors.New("disk full")
	wantStatusCode(t, srv.MatterWrite(context.Background(), tsuic.AttrKeypadLockout, uint8(4)), im.StatusFailure, "")
	if err := srv.Set(tsuic.AttrKeypadLockout, tsuic.KeypadLockoutLockout4); err == nil {
		t.Error("a local change the settings refused reported no error")
	}
	if err := srv.Set(tsuic.AttrKeypadLockout, uint8(99)); err == nil {
		t.Error("an undefined KeypadLockout accepted")
	}
	settings.values["ui.1"] = "x"
	if _, err := thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{Settings: settings, SettingKey: "ui.1"}); err == nil {
		t.Error("a stored KeypadLockout that is not a number accepted")
	}
}
