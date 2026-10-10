// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/pump"
	hepaspec "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// --- device: a ceiling fan -------------------------------------------------

// demoFan is a ceiling fan with ten speeds, an Auto mode and the Step
// command. The FanControl server owns the coupling rules between FanMode,
// PercentSetting and SpeedSetting; the device decides only what the rules
// leave to the manufacturer — which percentage Low, Medium and High mean —
// and reports where it ended up.
//
// The fan has no spin-up time: the current values follow the settings
// inside the write.
type demoFan struct {
	name string
	notifier
	version cluster.DataVersionTracker

	mu    sync.Mutex
	state fan.State
}

var (
	_ contract.EndpointSource = (*demoFan)(nil)
	_ contract.ChangeNotifier = (*demoFan)(nil)
	_ fan.StateSource         = (*demoFan)(nil)
)

// fanSpeedMax is SpeedMax: ten speeds, so one speed is ten percent.
const fanSpeedMax uint8 = 10

func newDemoFan(name string) *demoFan {
	zero := uint8(0)
	return &demoFan{name: name, state: fan.State{
		FanMode: fan.FanModeOff, PercentSetting: &zero, SpeedSetting: &zero,
	}}
}

// MatterDeviceType implements [contract.EndpointSource].
func (f *demoFan) MatterDeviceType() uint16 { return fan.DeviceTypeFan }

// MatterClusterServers implements [contract.EndpointSource].
func (f *demoFan) MatterClusterServers() []contract.ClusterServer {
	srv, err := fan.NewServer(fan.Config{
		Source:      f,
		Features:    fan.FeatureMultiSpeed | fan.FeatureAuto | fan.FeatureStep,
		Sequence:    fan.SequenceOffLowMedHighAuto,
		SpeedMax:    fanSpeedMax,
		DeviceType:  fan.DeviceTypeFan,
		DataVersion: &f.version,
	})
	if err != nil {
		panic(fmt.Sprintf("fan FanControl: %v", err))
	}
	return []contract.ClusterServer{srv}
}

// FanState implements [fan.StateSource].
func (f *demoFan) FanState() fan.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state
	st.PercentSetting = copyUint8(f.state.PercentSetting)
	st.SpeedSetting = copyUint8(f.state.SpeedSetting)
	return st
}

// fanModePercent is this fan's own meaning of the three named speeds.
var fanModePercent = map[fan.FanMode]uint8{
	fan.FanModeLow: 30, fan.FanModeMedium: 60, fan.FanModeHigh: 100,
}

// ApplyFanSettings implements [fan.StateSource]. A nil field is one the
// cluster's rules leave to the device; the device fills it from its own
// mapping and reports the outcome through FanState.
func (f *demoFan) ApplyFanSettings(_ context.Context, s fan.Settings) error {
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
	if s.PercentSetting != nil {
		st.PercentSetting = settingPtr(*s.PercentSetting)
	}
	if s.SpeedSetting != nil {
		st.SpeedSetting = settingPtr(*s.SpeedSetting)
	}
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

func settingPtr(s fan.Setting) *uint8 {
	if s.Null {
		return nil
	}
	v := s.Value
	return &v
}

func percentToSpeed(p uint8) uint8 {
	return uint8(min((uint16(p)*uint16(fanSpeedMax)+99)/100, uint16(fanSpeedMax))) //nolint:gosec // bounded by fanSpeedMax, a uint8
}

func percentToMode(p uint8) fan.FanMode {
	switch {
	case p <= 33:
		return fan.FanModeLow
	case p <= 66:
		return fan.FanModeMedium
	default:
		return fan.FanModeHigh
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

// demoThermostat is a heating/cooling thermostat. The module's Thermostat
// server holds its setpoints itself (cluster/thermo is a conformance
// reference that drives no device); this endpoint puts it in front of a real
// controller. The room temperature is the one thing the device reports, via
// the control hook.
//
// It also serves EnergyPreference with the EnergyBalance feature, which the
// Thermostat device type offers as optional (parity/schema.json): three
// balances from efficient to comfortable, and the two priorities they
// trade between.
type demoThermostat struct {
	name string
	once sync.Once
	srv  *thermo.ThermostatServer
	pref *energy.PreferenceServer
}

var (
	_ contract.EndpointSource  = (*demoThermostat)(nil)
	_ energy.PreferenceChanger = (*demoThermostat)(nil)
)

// deviceTypeThermostat is Thermostat (matter.js thermostat.element.ts).
const deviceTypeThermostat uint16 = 0x0301

func newDemoThermostat(name string) *demoThermostat { return &demoThermostat{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (t *demoThermostat) MatterDeviceType() uint16 { return deviceTypeThermostat }

// MatterClusterServers implements [contract.EndpointSource]. The server
// holds the setpoints, so the same instance serves every reassembly.
func (t *demoThermostat) MatterClusterServers() []contract.ClusterServer {
	t.build()
	return []contract.ClusterServer{t.srv, t.pref}
}

func (t *demoThermostat) build() {
	t.once.Do(func() {
		cfg := thermo.DefaultThermostatConfig()
		cfg.Features = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL
		t.srv = thermo.NewThermostatServer(cfg)
		room := int16(2150)
		t.srv.SetLocalTemperature(&room)
		efficient, comfort := "Efficient", "Comfort"
		pref, err := energy.NewEnergyPreference(energy.PreferenceConfig{
			Features: energy.PreferenceFeatureEnergyBalance,
			EnergyBalances: []energy.Balance{
				{Step: 0, Label: &efficient}, {Step: 50}, {Step: 100, Label: &comfort},
			},
			EnergyPriorities:     []energy.Priority{energy.PriorityEfficiency, energy.PriorityComfort},
			CurrentEnergyBalance: 1,
			Changer:              t,
		})
		if err != nil {
			panic(fmt.Sprintf("thermostat EnergyPreference: %v", err))
		}
		t.pref = pref
	})
}

// ChangePreference implements [energy.PreferenceChanger]: the thermostat
// takes every balance the server has let through.
func (t *demoThermostat) ChangePreference(_ context.Context, attrID uint32, index uint8) error {
	slog.Info("thermostat.preference", slog.String("device", t.name), slog.Int("attribute", int(attrID)), slog.Int("index", int(index)))
	return nil
}

// reportTemperature is the device's room sensor reporting, in 0.01 °C.
func (t *demoThermostat) reportTemperature(centi int16) {
	t.build()
	t.srv.SetLocalTemperature(&centi)
	slog.Info("thermostat.temperature", slog.String("device", t.name), slog.Int("centi_celsius", int(centi)))
}

// --- device: a circulation pump ------------------------------------------------

// demoPump is a circulation pump: OnOff powers it (Pump mandates OnOff),
// PumpConfigurationAndControl reports and sets how it runs, and a
// FlowMeasurement on the same endpoint reports what it moves — one of the
// optional servers the Pump device type lists.
type demoPump struct {
	name string
	notifier
	version cluster.DataVersionTracker
	flow    *demoReading

	mu   sync.Mutex
	on   bool
	mode pump.OperationMode

	srvMu sync.Mutex
	srv   *pump.Server
}

var (
	_ contract.EndpointSource = (*demoPump)(nil)
	_ contract.ChangeNotifier = (*demoPump)(nil)
	_ pump.StateSource        = (*demoPump)(nil)
	_ onOffDevice             = (*demoPump)(nil)
)

func newDemoPump(name string) *demoPump {
	return &demoPump{name: name, flow: newDemoReading(name+" flow", contract.MeasurementFlow, 1.5)}
}

// MatterDeviceType implements [contract.EndpointSource].
func (p *demoPump) MatterDeviceType() uint16 { return pump.DeviceTypePump }

// MatterClusterServers implements [contract.EndpointSource].
func (p *demoPump) MatterClusterServers() []contract.ClusterServer {
	maxPressure, maxSpeed, maxFlow := int16(3000), uint16(3200), uint16(60)
	minSpeed, maxConstSpeed := uint16(500), uint16(3200)
	srv, err := pump.NewServer(pump.Config{
		Source:   p,
		Features: pump.FeatureConstantSpeed,
		Optional: pump.OptionalPumpStatus,
		Limits: pump.Limits{
			MaxPressure: &maxPressure, MaxSpeed: &maxSpeed, MaxFlow: &maxFlow,
			MinConstSpeed: &minSpeed, MaxConstSpeed: &maxConstSpeed,
		},
		Events:      []uint32{pump.EventDryRunning, pump.EventPumpBlocked},
		DataVersion: &p.version,
	})
	if err != nil {
		panic(fmt.Sprintf("pump PumpConfigurationAndControl: %v", err))
	}
	p.srvMu.Lock()
	p.srv = srv
	p.srvMu.Unlock()
	return []contract.ClusterServer{
		&onOffServer{dev: p, logMessage: "pump.set"},
		srv,
		measurement.NewFlowServer(p.flow),
	}
}

// PumpState implements [pump.StateSource].
func (p *demoPump) PumpState() pump.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	var speed uint16
	if p.on {
		speed = map[pump.OperationMode]uint16{
			pump.OperationNormal: 2000, pump.OperationMinimum: 500, pump.OperationMaximum: 3200,
		}[p.mode]
	}
	return pump.State{
		EffectiveOperationMode: p.mode,
		EffectiveControlMode:   pump.ControlConstantSpeed,
		Speed:                  &speed,
		OperationMode:          p.mode,
		ControlMode:            pump.ControlConstantSpeed,
	}
}

// SetOperationMode implements [pump.StateSource].
func (p *demoPump) SetOperationMode(_ context.Context, mode pump.OperationMode) error {
	p.mu.Lock()
	p.mode = mode
	p.mu.Unlock()
	slog.Info("pump.mode", slog.String("device", p.name), slog.Int("mode", int(mode)))
	p.notify()
	return nil
}

func (p *demoPump) deviceName() string { return p.name }

func (p *demoPump) isOn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.on
}

func (p *demoPump) setOn(on bool) {
	p.mu.Lock()
	p.on = on
	p.mu.Unlock()
	p.notify()
}

// reportEvent is the pump raising one of its declared fault events.
func (p *demoPump) reportEvent(event uint32) error {
	p.srvMu.Lock()
	srv := p.srv
	p.srvMu.Unlock()
	if srv == nil {
		return fmt.Errorf("pump %s is not mounted yet", p.name)
	}
	slog.Info("pump.event", slog.String("device", p.name), slog.Int("event", int(event)))
	return srv.Emit(event)
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
	hepa   *filter.Server
	carbon *filter.Server
}

var _ contract.EndpointSource = (*demoAirPurifier)(nil)

func newDemoAirPurifier(name string) *demoAirPurifier {
	p := &demoAirPurifier{demoFan: newDemoFan(name)}
	products := []filter.ReplacementProduct{{ProductIdentifierType: hepaspec.ProductIdentifierTypeEan, ProductIdentifierValue: "1234567890123"}}
	optional := filter.OptionalInPlaceIndicator | filter.OptionalLastChangedTime | filter.OptionalResetCondition
	var err error
	p.hepa, err = filter.NewHepaFilterMonitoring(filter.Config{
		Features:             filter.FeatureCondition | filter.FeatureWarning | filter.FeatureReplacementProductList,
		Optional:             optional,
		DegradationDirection: hepaspec.DegradationDirectionDown,
		ReplacementProducts:  products,
		Initial:              filter.State{Condition: 20, ChangeIndication: filter.ChangeIndicationWarning, InPlaceIndicator: true},
		Resetter:             filterReset{name: name + " HEPA filter"},
	})
	if err != nil {
		panic(fmt.Sprintf("air purifier HEPA filter: %v", err))
	}
	p.carbon, err = filter.NewActivatedCarbonFilterMonitoring(filter.Config{
		Features:             filter.FeatureCondition | filter.FeatureReplacementProductList,
		Optional:             optional,
		DegradationDirection: hepaspec.DegradationDirectionDown,
		ReplacementProducts:  products,
		Initial:              filter.State{Condition: 20, ChangeIndication: filter.ChangeIndicationCritical, InPlaceIndicator: true},
		Resetter:             filterReset{name: name + " carbon filter"},
	})
	if err != nil {
		panic(fmt.Sprintf("air purifier carbon filter: %v", err))
	}
	return p
}

// MatterDeviceType implements [contract.EndpointSource].
func (p *demoAirPurifier) MatterDeviceType() uint16 { return fan.DeviceTypeAirPurifier }

// MatterClusterServers implements [contract.EndpointSource].
func (p *demoAirPurifier) MatterClusterServers() []contract.ClusterServer {
	srv, err := fan.NewServer(fan.Config{
		Source:      p.demoFan,
		Features:    fan.FeatureMultiSpeed | fan.FeatureAuto | fan.FeatureStep,
		Sequence:    fan.SequenceOffLowMedHighAuto,
		SpeedMax:    fanSpeedMax,
		DeviceType:  fan.DeviceTypeAirPurifier,
		DataVersion: &p.version,
	})
	if err != nil {
		panic(fmt.Sprintf("air purifier FanControl: %v", err))
	}
	return []contract.ClusterServer{srv, p.hepa, p.carbon}
}

// filterReset is the purifier's side of ResetCondition: a new filter is in.
type filterReset struct{ name string }

func (f filterReset) ResetCondition(context.Context) error {
	slog.Info("filter.replaced", slog.String("filter", f.name))
	return nil
}
