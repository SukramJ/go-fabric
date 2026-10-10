// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// The kitchen appliances: a dishwasher with its alarm, an oven with a
// heating cabinet, a microwave oven, and a laundry dryer. Like the washer
// (fleet_appliances.go) each builds its servers once and returns the same
// instances on every reassembly; what they do on a command is this host's
// own, the clusters' rules are the servers'.

// Device types (parity/schema.json).
const (
	deviceTypeOven          uint16 = 0x007B
	deviceTypeMicrowaveOven uint16 = 0x0079
)

// conditionHeater is the TemperatureControlledCabinet condition a heating
// compartment states; with it the cabinet's OvenMode and
// OvenCavityOperationalState ("[Heater]") are in scope, and the Oven's
// descendant requirement is met.
const conditionHeater = "Heater"

// cycleStates are the OperationalStateList of an appliance that starts and
// stops a cycle.
func cycleStates() []opstate.StateEntry {
	return []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StateError}}
}

// cycle is a Start / Stop handler for an OperationalState server: Start
// runs, Stop stops, and stopping a running cycle reports its completion.
type cycle struct {
	name string
	ops  func() *opstate.Server
}

func (c cycle) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	srv := c.ops()
	switch cmd {
	case opstate.CommandStart:
		if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
			return opstate.ErrorState{}, err
		}
	case opstate.CommandStop:
		raw, _ := srv.MatterRead(opstate.AttrOperationalState)
		if err := srv.SetOperationalState(opstate.StateStopped); err != nil {
			return opstate.ErrorState{}, err
		}
		if st, _ := raw.(uint8); opstate.State(st) == opstate.StateRunning {
			if err := srv.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorNoError}); err != nil {
				return opstate.ErrorState{}, err
			}
		}
	default:
		return opstate.ErrorState{ID: opstate.ErrorCommandInvalidInState}, nil
	}
	slog.Info("appliance.command", slog.String("device", c.name), slog.Int("command", int(cmd)))
	return opstate.ErrorState{ID: opstate.ErrorNoError}, nil
}

// --- device: a dishwasher ---------------------------------------------------

// The dishwasher's alarms: the four it can detect, of which a blocked
// inflow and a blocked drain latch until reset.
const (
	dishwasherAlarms = uint32(dwa.AlarmInflowError | dwa.AlarmDrainError | dwa.AlarmDoorError | dwa.AlarmTempTooHigh)
	dishwasherLatch  = uint32(dwa.AlarmInflowError | dwa.AlarmDrainError)
)

// demoDishwasher is a dishwasher: OperationalState runs the programme, and
// DishwasherAlarm reports its faults (with Reset and ModifyEnabledAlarms).
type demoDishwasher struct {
	name string

	once    sync.Once
	ops     *opstate.Server
	alarms  *alarmbase.Server
	version struct{ ops, alarms cluster.DataVersionTracker }
}

var (
	_ contract.EndpointSource = (*demoDishwasher)(nil)
	_ alarmbase.Delegate      = (*demoDishwasher)(nil)
)

func newDemoDishwasher(name string) *demoDishwasher { return &demoDishwasher{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (d *demoDishwasher) MatterDeviceType() uint16 { return opstate.DeviceTypeDishwasher }

// MatterClusterServers implements [contract.EndpointSource].
func (d *demoDishwasher) MatterClusterServers() []contract.ClusterServer {
	d.build()
	return []contract.ClusterServer{d.ops, d.alarms}
}

func (d *demoDishwasher) build() {
	d.once.Do(func() {
		ops, err := opstate.NewServer(opstate.Config{
			Handler:     cycle{name: d.name, ops: func() *opstate.Server { return d.ops }},
			Commands:    opstate.CommandStart | opstate.CommandStop,
			States:      cycleStates(),
			State:       opstate.StateStopped,
			DeviceType:  opstate.DeviceTypeDishwasher,
			DataVersion: &d.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("dishwasher OperationalState: %v", err))
		}
		alarms, err := alarmbase.NewDishwasherAlarm(alarmbase.Config{
			Features:            alarmbase.FeatureReset,
			Supported:           dishwasherAlarms,
			Latch:               dishwasherLatch,
			Mask:                dishwasherAlarms,
			ModifyEnabledAlarms: true,
			Delegate:            d,
			DataVersion:         &d.version.alarms,
		})
		if err != nil {
			panic(fmt.Sprintf("dishwasher DishwasherAlarm: %v", err))
		}
		d.ops, d.alarms = ops, alarms
	})
}

// ModifyEnabledAlarms implements [alarmbase.Delegate]: the dishwasher
// monitors whatever the controller enables.
func (d *demoDishwasher) ModifyEnabledAlarms(_ context.Context, mask uint32) error {
	slog.Info("dishwasher.alarm_mask", slog.String("device", d.name), slog.Uint64("mask", uint64(mask)))
	return nil
}

// ResetAlarms implements [alarmbase.Delegate]: the dishwasher clears the
// fault conditions it was asked to.
func (d *demoDishwasher) ResetAlarms(_ context.Context, alarms uint32) error {
	slog.Info("dishwasher.alarm_reset", slog.String("device", d.name), slog.Uint64("alarms", uint64(alarms)))
	return nil
}

// --- device: an oven --------------------------------------------------------

// Oven modes (OvenMode SupportedModes); the numbers are this host's own.
const (
	ovenBake       uint8 = 0
	ovenConvection uint8 = 1
)

// Oven cavity temperatures, in 0.01 °C: 50 °C to 250 °C.
const (
	ovenMin      int16 = 5000
	ovenMax      int16 = 25000
	ovenSetpoint int16 = 18000
)

// demoOven is an oven: its own endpoint serves nothing beyond the
// Descriptor, and its one cavity — a TemperatureControlledCabinet part
// stating the Heater condition — serves OvenMode,
// OvenCavityOperationalState and TemperatureControl.
type demoOven struct {
	name   string
	cavity *demoOvenCavity
}

var _ contract.EndpointSource = (*demoOven)(nil)

func newDemoOven(name string) *demoOven {
	return &demoOven{name: name, cavity: &demoOvenCavity{name: name + " cavity"}}
}

// MatterDeviceType implements [contract.EndpointSource].
func (o *demoOven) MatterDeviceType() uint16 { return deviceTypeOven }

// MatterClusterServers implements [contract.EndpointSource]: the Oven
// device type mandates no application cluster of its own.
func (o *demoOven) MatterClusterServers() []contract.ClusterServer { return nil }

// parts is the oven's cavity.
func (o *demoOven) parts() []endpoint.Spec {
	return []endpoint.Spec{{
		StableKey:        endpoint.StringKey("demo:oven:cavity"),
		DeviceType:       deviceTypeTemperatureControlledCabinet,
		Source:           o.cavity,
		DeviceConditions: []string{conditionHeater},
	}}
}

// demoOvenCavity is the oven's heating compartment.
type demoOvenCavity struct {
	name string

	once    sync.Once
	modes   *modebase.Server
	ops     *opstate.Server
	temp    *thermo.TemperatureControlServer
	version struct{ modes, ops, temp cluster.DataVersionTracker }
}

var (
	_ contract.EndpointSource  = (*demoOvenCavity)(nil)
	_ modebase.ModeChanger     = (*demoOvenCavity)(nil)
	_ thermo.TemperatureSetter = (*demoOvenCavity)(nil)
)

// MatterDeviceType implements [contract.EndpointSource].
func (c *demoOvenCavity) MatterDeviceType() uint16 { return deviceTypeTemperatureControlledCabinet }

// MatterClusterServers implements [contract.EndpointSource].
func (c *demoOvenCavity) MatterClusterServers() []contract.ClusterServer {
	c.build()
	return []contract.ClusterServer{c.modes, c.ops, c.temp}
}

func (c *demoOvenCavity) build() {
	c.once.Do(func() {
		modes, err := modebase.NewOvenMode(modebase.Config{
			Changer: c,
			SupportedModes: []modebase.ModeOption{
				{Label: "Bake", Mode: ovenBake, Tags: []modebase.ModeTag{{Value: modebase.OvenTagBake}}},
				{Label: "Convection", Mode: ovenConvection, Tags: []modebase.ModeTag{{Value: modebase.OvenTagConvection}}},
			},
			CurrentMode: ovenBake,
			DataVersion: &c.version.modes,
		})
		if err != nil {
			panic(fmt.Sprintf("oven OvenMode: %v", err))
		}
		phase := uint8(0)
		ops, err := opstate.NewOvenCavityServer(opstate.Config{
			Handler:      cycle{name: c.name, ops: func() *opstate.Server { return c.ops }},
			Commands:     opstate.CommandStart | opstate.CommandStop,
			States:       cycleStates(),
			State:        opstate.StateStopped,
			Phases:       opstate.OvenCavityPhases,
			CurrentPhase: &phase,
			DeviceType:   opstate.DeviceTypeTemperatureControlledCabinet,
			DataVersion:  &c.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("oven OvenCavityOperationalState: %v", err))
		}
		temp, err := thermo.NewTemperatureControl(thermo.TemperatureControlConfig{
			Features:            thermo.TemperatureControlFeatureNumber,
			Setter:              c,
			MinTemperature:      ovenMin,
			MaxTemperature:      ovenMax,
			TemperatureSetpoint: ovenSetpoint,
			DataVersion:         &c.version.temp,
		})
		if err != nil {
			panic(fmt.Sprintf("oven TemperatureControl: %v", err))
		}
		c.modes, c.ops, c.temp = modes, ops, temp
	})
}

// ChangeToMode implements [modebase.ModeChanger].
func (c *demoOvenCavity) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	slog.Info("oven.mode", slog.String("device", c.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

// SetTemperature implements [thermo.TemperatureSetter].
func (c *demoOvenCavity) SetTemperature(_ context.Context, target *int16, _ *uint8) error {
	if target != nil {
		slog.Info("oven.setpoint", slog.String("device", c.name), slog.Int("centi_celsius", int(*target)))
	}
	return nil
}

// --- device: a microwave oven -----------------------------------------------

// Microwave modes (MicrowaveOvenMode SupportedModes); the numbers are this
// host's own, Normal is the tag MicrowaveOvenControl falls back on.
const (
	microwaveNormal  uint8 = 0
	microwaveDefrost uint8 = 1
)

// microwaveMaxCookTime is the longest cook time the microwave takes, in s.
const microwaveMaxCookTime uint32 = 3600

// demoMicrowave is a microwave oven: OperationalState runs it,
// MicrowaveOvenMode picks Normal or Defrost, MicrowaveOvenControl sets
// cook time and power.
type demoMicrowave struct {
	name string

	once    sync.Once
	ops     *opstate.Server
	modes   *modebase.Server
	control *appliance.MicrowaveServer
	version struct{ ops, modes, control cluster.DataVersionTracker }
}

var (
	_ contract.EndpointSource = (*demoMicrowave)(nil)
	_ appliance.Oven          = (*demoMicrowave)(nil)
	_ opstate.CommandHandler  = (*demoMicrowave)(nil)
)

func newDemoMicrowave(name string) *demoMicrowave { return &demoMicrowave{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (m *demoMicrowave) MatterDeviceType() uint16 { return deviceTypeMicrowaveOven }

// MatterClusterServers implements [contract.EndpointSource].
func (m *demoMicrowave) MatterClusterServers() []contract.ClusterServer {
	m.build()
	return []contract.ClusterServer{m.ops, m.modes, m.control}
}

func (m *demoMicrowave) build() {
	m.once.Do(func() {
		ops, err := opstate.NewServer(opstate.Config{
			Handler:  m,
			Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandStart | opstate.CommandStop,
			States: []opstate.StateEntry{
				{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StatePaused}, {ID: opstate.StateError},
			},
			State:       opstate.StateStopped,
			DeviceType:  opstate.DeviceTypeMicrowaveOven,
			DataVersion: &m.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("microwave OperationalState: %v", err))
		}
		modes, err := modebase.NewMicrowaveOvenMode(modebase.Config{
			SupportedModes: []modebase.ModeOption{
				{Label: "Normal", Mode: microwaveNormal, Tags: []modebase.ModeTag{{Value: modebase.MicrowaveTagNormal}}},
				{Label: "Defrost", Mode: microwaveDefrost, Tags: []modebase.ModeTag{{Value: modebase.MicrowaveTagDefrost}}},
			},
			CurrentMode: microwaveNormal,
			DataVersion: &m.version.modes,
		})
		if err != nil {
			panic(fmt.Sprintf("microwave MicrowaveOvenMode: %v", err))
		}
		control, err := appliance.NewMicrowaveOvenControl(appliance.MicrowaveConfig{
			Features:     appliance.MicrowaveFeaturePowerAsNumber,
			Oven:         m,
			AddMoreTime:  true,
			MaxCookTime:  microwaveMaxCookTime,
			PowerSetting: appliance.DefaultMaxPower,
			DataVersion:  &m.version.control,
		})
		if err != nil {
			panic(fmt.Sprintf("microwave MicrowaveOvenControl: %v", err))
		}
		m.ops, m.modes, m.control = ops, modes, control
	})
}

// OperationalState implements [appliance.Oven].
func (m *demoMicrowave) OperationalState() uint8 {
	raw, _ := m.ops.MatterRead(opstate.AttrOperationalState)
	st, _ := raw.(uint8)
	return st
}

// NormalMode implements [appliance.Oven]: the mode tagged Normal.
func (m *demoMicrowave) NormalMode() (uint8, bool) { return microwaveNormal, true }

// SupportedMode implements [appliance.Oven].
func (m *demoMicrowave) SupportedMode(mode uint8) bool {
	return mode == microwaveNormal || mode == microwaveDefrost
}

// StartSupported implements [appliance.Oven]: the OperationalState server
// accepts Start.
func (m *demoMicrowave) StartSupported() bool { return true }

// SetCookingParameters implements [appliance.Oven]: the microwave takes the
// mode and, when asked, starts.
func (m *demoMicrowave) SetCookingParameters(_ context.Context, mode uint8, cookTime uint32, start bool, power, _ *uint8) error {
	if err := m.modes.SetCurrentMode(mode); err != nil {
		return err
	}
	attrs := []any{slog.String("device", m.name), slog.Int("mode", int(mode)), slog.Uint64("cook_time", uint64(cookTime)), slog.Bool("start", start)}
	if power != nil {
		attrs = append(attrs, slog.Int("power", int(*power)))
	}
	slog.Info("microwave.parameters", attrs...)
	if start {
		return m.run(cookTime)
	}
	return nil
}

// ModifyCookTime implements [appliance.Oven].
func (m *demoMicrowave) ModifyCookTime(_ context.Context, cookTime uint32) error {
	slog.Info("microwave.cook_time", slog.String("device", m.name), slog.Uint64("cook_time", uint64(cookTime)))
	if m.OperationalState() == uint8(opstate.StateRunning) {
		return m.ops.SetCountdownTime(&cookTime)
	}
	return nil
}

// run starts cooking for cookTime seconds.
func (m *demoMicrowave) run(cookTime uint32) error {
	if err := m.ops.SetOperationalState(opstate.StateRunning); err != nil {
		return err
	}
	return m.ops.SetCountdownTime(&cookTime)
}

// HandleOperationalCommand implements [opstate.CommandHandler].
func (m *demoMicrowave) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	var err error
	switch cmd {
	case opstate.CommandStart:
		err = m.run(m.control.CookTime())
	case opstate.CommandResume:
		err = m.ops.SetOperationalState(opstate.StateRunning)
	case opstate.CommandPause:
		err = m.ops.SetOperationalState(opstate.StatePaused)
	case opstate.CommandStop:
		running := m.OperationalState() != uint8(opstate.StateStopped)
		if err = m.ops.SetOperationalState(opstate.StateStopped); err == nil {
			err = m.ops.SetCountdownTime(nil)
		}
		if err == nil && running {
			err = m.ops.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorNoError})
		}
	default:
		return opstate.ErrorState{ID: opstate.ErrorCommandInvalidInState}, nil
	}
	if err != nil {
		return opstate.ErrorState{}, err
	}
	slog.Info("microwave.command", slog.String("device", m.name), slog.Int("command", int(cmd)))
	return opstate.ErrorState{ID: opstate.ErrorNoError}, nil
}

// --- device: a laundry dryer ------------------------------------------------

// demoDryer is a laundry dryer: OperationalState runs the cycle,
// LaundryDryerControls picks how dry.
type demoDryer struct {
	name string

	once     sync.Once
	ops      *opstate.Server
	controls *appliance.DryerServer
	version  struct{ ops, controls cluster.DataVersionTracker }
}

var (
	_ contract.EndpointSource = (*demoDryer)(nil)
	_ appliance.DryerListener = (*demoDryer)(nil)
)

func newDemoDryer(name string) *demoDryer { return &demoDryer{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (d *demoDryer) MatterDeviceType() uint16 { return opstate.DeviceTypeLaundryDryer }

// MatterClusterServers implements [contract.EndpointSource].
func (d *demoDryer) MatterClusterServers() []contract.ClusterServer {
	d.build()
	return []contract.ClusterServer{d.ops, d.controls}
}

func (d *demoDryer) build() {
	d.once.Do(func() {
		ops, err := opstate.NewServer(opstate.Config{
			Handler:     cycle{name: d.name, ops: func() *opstate.Server { return d.ops }},
			Commands:    opstate.CommandStart | opstate.CommandStop,
			States:      cycleStates(),
			State:       opstate.StateStopped,
			DeviceType:  opstate.DeviceTypeLaundryDryer,
			DataVersion: &d.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("dryer OperationalState: %v", err))
		}
		normal := appliance.DrynessLevel(1) // Normal
		controls, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{
			SupportedDrynessLevels: []appliance.DrynessLevel{0, 1, 2}, // Low, Normal, Extra
			SelectedDrynessLevel:   &normal,
			Listener:               d,
			DataVersion:            &d.version.controls,
		})
		if err != nil {
			panic(fmt.Sprintf("dryer LaundryDryerControls: %v", err))
		}
		d.ops, d.controls = ops, controls
	})
}

// SelectedDrynessLevelChanged implements [appliance.DryerListener].
func (d *demoDryer) SelectedDrynessLevelChanged(level *appliance.DrynessLevel) {
	v := -1
	if level != nil {
		v = int(*level)
	}
	slog.Info("dryer.dryness", slog.String("device", d.name), slog.Int("level", v))
}
