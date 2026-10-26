// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	acfdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	epdef "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// --- device: a ceiling fan -------------------------------------------------

// demoFan is a ceiling fan with ten speeds, an Auto mode and the Step
// command. The kit's FanControl server owns the coupling rules between
// FanMode, PercentSetting and SpeedSetting; the device decides only what
// the rules leave to the manufacturer — which percentage Low, Medium and
// High mean — and reports where it ended up.
//
// The fan has no spin-up time: the current values follow the settings
// inside the write.
type demoFan struct {
	name string
	notifier

	mu    sync.Mutex
	state fandef.State
}

var (
	_ contract.ChangeNotifier = (*demoFan)(nil)
	_ fandef.StateSource      = (*demoFan)(nil)
)

// fanSpeedMax is SpeedMax: ten speeds, so one speed is ten percent.
const fanSpeedMax uint8 = 10

func newDemoFan(name string) *demoFan {
	zero := uint8(0)
	return &demoFan{name: name, state: fandef.State{
		FanMode: fandef.FanModeOff, PercentSetting: &zero, SpeedSetting: &zero,
	}}
}

// declaration declares the fan's FanControl: MultiSpeed, Auto and Step,
// the Off/Low/Med/High/Auto sequence, ten speeds, the fan as the kit's
// server's StateSource.
func (f *demoFan) declaration() device.Declaration {
	sequence, speeds := fandef.FanModeSequenceOffLowMedHighAuto, fanSpeedMax
	return fandef.Declare{
		Features:    fandef.FeatureMultiSpeed | fandef.FeatureAuto | fandef.FeatureStep,
		StateSource: f,
		Initial:     fandef.Patch{FanModeSequence: &sequence, SpeedMax: &speeds},
	}
}

// FanState implements [fandef.StateSource].
func (f *demoFan) FanState() fandef.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state
	st.PercentSetting = copyUint8(f.state.PercentSetting)
	st.SpeedSetting = copyUint8(f.state.SpeedSetting)
	return st
}

// fanModePercent is this fan's own meaning of the three named speeds.
var fanModePercent = map[fandef.FanModeEnum]uint8{
	fandef.FanModeLow: 30, fandef.FanModeMedium: 60, fandef.FanModeHigh: 100,
}

// ApplyFanSettings implements [fandef.StateSource]. A nil field is one the
// cluster's rules leave to the device; the device fills it from its own
// mapping and reports the outcome through FanState.
func (f *demoFan) ApplyFanSettings(_ context.Context, s fandef.Patch) error {
	f.mu.Lock()
	st := &f.state
	if s.FanMode != nil {
		st.FanMode = *s.FanMode
		if p, ok := fanModePercent[*s.FanMode]; ok {
			st.PercentSetting = &p
			speed := percentToSpeed(p)
			st.SpeedSetting = &speed
		}
	}
	st.PercentSetting = settingOf(st.PercentSetting, s.PercentSetting, fandef.AttrPercentSetting, s.Null)
	st.SpeedSetting = settingOf(st.SpeedSetting, s.SpeedSetting, fandef.AttrSpeedSetting, s.Null)
	// A percentage or speed with no mode decided by the rules lands on the
	// named speed the device associates with it.
	if s.FanMode == nil && st.PercentSetting != nil && *st.PercentSetting > 0 {
		st.FanMode = percentToMode(*st.PercentSetting)
	}
	if st.PercentSetting != nil {
		st.PercentCurrent = *st.PercentSetting
	}
	if st.SpeedSetting != nil {
		st.SpeedCurrent = *st.SpeedSetting
	}
	mode, percent := st.FanMode, st.PercentCurrent
	f.mu.Unlock()
	slog.Info("fan.set", slog.String("device", f.name), slog.Int("mode", int(mode)), slog.Int("percent", int(percent)))
	f.notify()
	return nil
}

// settingOf is the setting after a patch: null when the patch names the
// attribute in Null, the patched value when it sets one, else unchanged.
func settingOf(cur, v *uint8, attr uint32, null []uint32) *uint8 {
	switch {
	case slices.Contains(null, attr):
		return nil
	case v != nil:
		out := *v
		return &out
	default:
		return cur
	}
}

func percentToSpeed(p uint8) uint8 {
	return uint8(min((uint16(p)*uint16(fanSpeedMax)+99)/100, uint16(fanSpeedMax))) //nolint:gosec // bounded by fanSpeedMax, a uint8
}

func percentToMode(p uint8) fandef.FanModeEnum {
	switch {
	case p <= 33:
		return fandef.FanModeLow
	case p <= 66:
		return fandef.FanModeMedium
	default:
		return fandef.FanModeHigh
	}
}

func copyUint8(v *uint8) *uint8 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

// --- device: a thermostat ----------------------------------------------------

// demoThermostat is a heating/cooling thermostat. The device kit's
// Thermostat server holds its setpoints itself (cluster/thermo is a
// conformance reference that drives no device); this endpoint puts it in
// front of a real controller. The room temperature is the one thing the
// device reports, via the control hook.
//
// It also serves EnergyPreference with the EnergyBalance feature, which the
// Thermostat device type offers as optional (parity/schema.json): three
// balances from efficient to comfortable, and the two priorities they
// trade between.
//
// Its display settings are a ThermostatUserInterfaceConfiguration, the
// optional cluster the Thermostat device type offers (thermostat.element.ts),
// with matter.js's defaults; KeypadLockout persists in the daemon's
// settings.
//
// It serves Presets (PRES) in the shape of matter.js's CHIP test
// thermostat (support/chip-testing/src/AllClustersTestInstance.ts:1058-1103
// at 862ad75f): three preset types — UserDefined, Occupied, Unoccupied —
// of one preset each, Automatic and SupportsNames, NumberOfPresets 5, and
// two presets shipped with the device ("UD", UserDefined; "Occ", Occupied,
// built-in), kept in the daemon's store. TC-TSTAT-4.2 fills each scenario
// once and counts null — "no name" — as a name that may not repeat within
// a scenario, so a single scenario of five unnamed presets refuses its
// step 13. No Occupancy: the endpoint has no occupancy sensor.
//
// It serves the optional TemperatureSetpointHold (off: following its
// program; with it TemperatureSetpointHoldDuration, null) and
// ThermostatRunningState (idle), the attributes a week-program thermostat
// reports (cluster/thermo ThermostatConfig). A controller's hold write is
// stored: the endpoint declares no Sink.
type demoThermostat struct {
	name string

	// presets and settings are the daemon's store (attachStore); nil keeps
	// the presets and KeypadLockout in memory.
	presets  thermostatdef.PresetStore
	settings tuidef.SettingStore

	mu  sync.Mutex
	srv *thermostatdef.Server
}

// thermostatStoreKey names the thermostat's presets in the daemon's store:
// its device address.
const thermostatStoreKey = "demo-thermostat-1"

// attachStore persists the thermostat's presets and KeypadLockout in the
// daemon's store. Call it before the thermostat's device is built
// (fleet.endpoints).
func (t *demoThermostat) attachStore(presets thermostatdef.PresetStore, settings tuidef.SettingStore) {
	t.presets, t.settings = presets, settings
}

func newDemoThermostat(name string) *demoThermostat { return &demoThermostat{name: name} }

// device builds the Thermostat with its EnergyPreference and
// ThermostatUserInterfaceConfiguration.
func (t *demoThermostat) device(opts device.Options) (*device.Thermostat, error) {
	names := thermostatdef.PresetTypeFeaturesAutomatic | thermostatdef.PresetTypeFeaturesSupportsNames
	presetTypes := []thermostatdef.PresetTypeStruct{
		{PresetScenario: thermostatdef.PresetScenarioUserDefined, NumberOfPresets: 1, PresetTypeFeatures: names},
		{PresetScenario: thermostatdef.PresetScenarioOccupied, NumberOfPresets: 1, PresetTypeFeatures: names},
		{PresetScenario: thermostatdef.PresetScenarioUnoccupied, NumberOfPresets: 1, PresetTypeFeatures: names},
	}
	cool, heat := int16(2600), int16(2000)
	presets := []thermostatdef.PresetStruct{
		{
			PresetHandle: spec.ValueOf([]byte("UserDef")), PresetScenario: thermostatdef.PresetScenarioUserDefined,
			Name: &spec.Nullable[string]{Value: "UD"}, CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(false),
		},
		{
			PresetHandle: spec.ValueOf([]byte("TestOcc")), PresetScenario: thermostatdef.PresetScenarioOccupied,
			Name: &spec.Nullable[string]{Value: "Occ"}, CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(true),
		},
	}
	count, room := uint8(5), int16(2150)
	hold, idle := thermostatdef.TemperatureSetpointHoldSetpointHoldOff, thermostatdef.RelayStateBitmap(0)
	efficient, comfort := "Efficient", "Comfort"
	balances := []epdef.BalanceStruct{{Step: 0, Label: &efficient}, {Step: 50}, {Step: 100, Label: &comfort}}
	priorities := []epdef.EnergyPriorityEnum{epdef.EnergyPriorityEfficiency, epdef.EnergyPriorityComfort}
	balance := uint8(1)
	d, err := device.NewThermostat(opts,
		thermostatdef.Declare{
			Features:    thermostatdef.FeatureHeating | thermostatdef.FeatureCooling | thermostatdef.FeaturePresets,
			PresetStore: t.presets,
			Initial: thermostatdef.Patch{
				NumberOfPresets: &count, PresetTypes: &presetTypes, Presets: &presets, LocalTemperature: &room,
				TemperatureSetpointHold: &hold, ThermostatRunningState: &idle,
			},
		},
		epdef.Declare{
			Features: epdef.FeatureEnergyBalance,
			Initial:  epdef.Patch{EnergyBalances: &balances, EnergyPriorities: &priorities, CurrentEnergyBalance: &balance},
			// The thermostat takes every balance the server lets through.
			OnWrite: func(_ context.Context, next epdef.State, attrID uint32) error {
				slog.Info("thermostat.preference", slog.String("device", t.name), slog.Int("attribute", int(attrID)),
					slog.Int("index", int(next.CurrentEnergyBalance)))
				return nil
			},
		},
		tuidef.Declare{SettingStore: t.settings})
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.srv = d.Thermostat()
	t.mu.Unlock()
	return d, nil
}

// reportTemperature is the device's room sensor reporting, in 0.01 °C.
func (t *demoThermostat) reportTemperature(centi int16) {
	t.mu.Lock()
	srv := t.srv
	t.mu.Unlock()
	if srv != nil {
		if err := srv.Set(thermostatdef.Patch{LocalTemperature: &centi}); err != nil {
			slog.Warn("thermostat.temperature", slog.String("device", t.name), slog.Any("err", err))
		}
	}
	slog.Info("thermostat.temperature", slog.String("device", t.name), slog.Int("centi_celsius", int(centi)))
}

// --- device: a circulation pump ------------------------------------------------

// demoPump is a circulation pump: OnOff powers it (Pump mandates OnOff),
// PumpConfigurationAndControl reports and sets how it runs, and a
// FlowMeasurement on the same endpoint reports what it moves — one of the
// optional clusters the Pump device type lists.
type demoPump struct {
	name string
	notifier
	flow *demoReading

	mu   sync.Mutex
	on   bool
	mode pumpdef.OperationModeEnum
	dev  *device.Pump
}

var (
	_ contract.ChangeNotifier = (*demoPump)(nil)
	_ pumpdef.StateSource     = (*demoPump)(nil)
)

func newDemoPump(name string) *demoPump {
	return &demoPump{name: name, flow: newDemoReading(name+" flow", 1.5)}
}

// declarations declare the pump's clusters: PumpConfigurationAndControl
// at constant speed with PumpStatus, its limits and the two fault events,
// the pump as the kit's server's StateSource; and the flow it moves.
func (p *demoPump) declarations() []device.Option {
	maxPressure, maxSpeed, maxFlow := int16(3000), uint16(3200), uint16(60)
	minSpeed, maxConstSpeed := uint16(500), uint16(3200)
	var status pumpdef.PumpStatusBitmap
	return []device.Option{
		pumpdef.Declare{
			Features:    pumpdef.FeatureConstantSpeed,
			StateSource: p,
			Events:      []uint32{pumpdef.EventDryRunning, pumpdef.EventPumpBlocked},
			Initial: pumpdef.Patch{
				MaxPressure: &maxPressure, MaxSpeed: &maxSpeed, MaxFlow: &maxFlow,
				MinConstSpeed: &minSpeed, MaxConstSpeed: &maxConstSpeed,
				PumpStatus: &status, // served (OptionalPumpStatus); the source reports it
			},
		},
		p.flow.flowDeclaration(),
	}
}

// device builds the pump: Pump with the device layer's default OnOff
// server, which the pump follows through the device's OnChanged hook.
func (p *demoPump) device(opts device.Options) (*device.Pump, error) {
	dev, err := device.NewPump(opts, append(p.declarations(), device.WithChangeNotifier(p))...)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.dev = dev
	p.mu.Unlock()
	followOnOff(dev.Device, dev.OnOff(), func(on bool) {
		p.setOn(on)
		slog.Info("pump.set", slog.String("device", p.name), slog.Bool("on", on))
	})
	return dev, nil
}

// PumpState implements [pumpdef.StateSource].
func (p *demoPump) PumpState() pumpdef.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	var speed uint16
	if p.on {
		speed = map[pumpdef.OperationModeEnum]uint16{
			pumpdef.OperationModeNormal: 2000, pumpdef.OperationModeMinimum: 500, pumpdef.OperationModeMaximum: 3200,
		}[p.mode]
	}
	return pumpdef.State{
		EffectiveOperationMode: p.mode,
		EffectiveControlMode:   pumpdef.ControlModeConstantSpeed,
		Speed:                  &speed,
		OperationMode:          p.mode,
		ControlMode:            pumpdef.ControlModeConstantSpeed,
	}
}

// SetOperationMode implements [pumpdef.StateSource].
func (p *demoPump) SetOperationMode(_ context.Context, mode pumpdef.OperationModeEnum) error {
	p.mu.Lock()
	p.mode = mode
	p.mu.Unlock()
	slog.Info("pump.mode", slog.String("device", p.name), slog.Int("mode", int(mode)))
	p.notify()
	return nil
}

func (p *demoPump) setOn(on bool) {
	p.mu.Lock()
	p.on = on
	p.mu.Unlock()
	p.notify()
}

// reportEvent is the pump raising one of its declared fault events.
func (p *demoPump) reportEvent(event uint32) error {
	p.mu.Lock()
	dev := p.dev
	p.mu.Unlock()
	if dev == nil {
		return fmt.Errorf("pump %s is not built yet", p.name)
	}
	endpoint, ok := dev.EndpointNumber()
	if !ok {
		return fmt.Errorf("pump %s is not mounted yet", p.name)
	}
	slog.Info("pump.event", slog.String("device", p.name), slog.Int("event", int(event)))
	srv := dev.PumpConfigurationAndControl()
	switch event {
	case pumpdef.EventDryRunning:
		return srv.EmitDryRunning(endpoint, pumpdef.DryRunningEvent{})
	case pumpdef.EventPumpBlocked:
		return srv.EmitPumpBlocked(endpoint, pumpdef.PumpBlockedEvent{})
	default:
		return errors.New("the pump declares no such event")
	}
}

// --- device: an air purifier -------------------------------------------------

// demoAirPurifier is an air purifier: its fan (FanControl, mandatory for the
// AirPurifier device type) and two filters it monitors, a HEPA filter and an
// activated-carbon filter (HepaFilterMonitoring and
// ActivatedCarbonFilterMonitoring, both optional for the device type). The
// filters start where matter.js's all-clusters test app starts them
// (support/chip-testing/src/cluster/TestHEPAFilterMonitoringServer.ts and
// TestActivatedCarbonFilterMonitoringServer.ts): 20 % left, degrading
// downwards, one replacement product; the HEPA filter with the Warning
// feature in Warning, the carbon filter without it in Critical. Replacing a
// filter (ResetCondition) brings it back to 100 % and Ok.
type demoAirPurifier struct {
	*demoFan
}

func newDemoAirPurifier(name string) *demoAirPurifier {
	return &demoAirPurifier{demoFan: newDemoFan(name)}
}

// declarations declare the purifier's fan and its two filters.
func (p *demoAirPurifier) declarations() []device.Option {
	condition, inPlace := uint8(20), true
	hepaDown, carbonDown := hepadef.DegradationDirectionDown, acfdef.DegradationDirectionDown
	hepaWarning, carbonCritical := hepadef.ChangeIndicationWarning, acfdef.ChangeIndicationCritical
	hepaProducts := []hepadef.ReplacementProductStruct{{ProductIdentifierType: hepadef.ProductIdentifierTypeEan, ProductIdentifierValue: "1234567890123"}}
	carbonProducts := []acfdef.ReplacementProductStruct{{ProductIdentifierType: acfdef.ProductIdentifierTypeEan, ProductIdentifierValue: "1234567890123"}}
	return []device.Option{
		p.declaration(),
		hepadef.Declare{
			Features: hepadef.FeatureCondition | hepadef.FeatureWarning | hepadef.FeatureReplacementProductList,
			Commands: []uint32{hepadef.CmdResetCondition},
			Resetter: filterReset{name: p.name + " HEPA filter"},
			Initial: hepadef.Patch{
				Condition: &condition, DegradationDirection: &hepaDown, ChangeIndication: &hepaWarning,
				InPlaceIndicator: &inPlace, ReplacementProductList: &hepaProducts,
				Null: []uint32{hepadef.AttrLastChangedTime},
			},
		},
		acfdef.Declare{
			Features: acfdef.FeatureCondition | acfdef.FeatureReplacementProductList,
			Commands: []uint32{acfdef.CmdResetCondition},
			Resetter: filterReset{name: p.name + " carbon filter"},
			Initial: acfdef.Patch{
				Condition: &condition, DegradationDirection: &carbonDown, ChangeIndication: &carbonCritical,
				InPlaceIndicator: &inPlace, ReplacementProductList: &carbonProducts,
				Null: []uint32{acfdef.AttrLastChangedTime},
			},
		},
	}
}

// filterReset is the purifier's side of ResetCondition: a new filter is in.
type filterReset struct{ name string }

func (f filterReset) ResetCondition(context.Context) error {
	slog.Info("filter.replaced", slog.String("filter", f.name))
	return nil
}
