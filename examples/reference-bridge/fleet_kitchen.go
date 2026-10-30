// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"

	dwadef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	ldcdef "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	mwocdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	mwomdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	ovendef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	ovenmdef "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/device"
)

// The kitchen appliances: a dishwasher with its alarm, an oven with a
// heating cabinet, a microwave oven, and a laundry dryer. Like the washer
// (fleet_appliances.go) each declares its clusters on the device kit's
// servers; what they do on a command is this host's own, the clusters'
// rules are the servers'.

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

// ovenCavityPhases are the PhaseList entries OvenCavityOperationalState
// allows (matter.js OvenCavityOperationalStateServer.ts:24, :35-46).
var ovenCavityPhases = []string{"pre-heating", "pre-heated", "cooling down"}

// cycleStates are the OperationalStateList of an appliance that starts and
// stops a cycle.
func cycleStates() *[]opdef.OperationalStateStruct {
	return &[]opdef.OperationalStateStruct{
		{OperationalStateId: opdef.OperationalStateStopped},
		{OperationalStateId: opdef.OperationalStateRunning},
		{OperationalStateId: opdef.OperationalStateError},
	}
}

// cycle is a Start / Stop handler for an OperationalState derivation's
// server: Start runs, Stop stops, and stopping a running cycle reports its
// completion. Start while OperationalError is not NoError answers
// UnableToStartOrResume without changing state, as CHIP's all-clusters
// oven does (oven-operational-state-delegate.cpp HandleStartStateCallback
// :139-146 at the harness pin; TC-OVENOPSTATE-2.2 step 17). The three
// derivations share the command ids and the state and error values the
// cycle reads and sets; the functions reach its typed server.
type cycle struct {
	name string
	// state reads OperationalState and OperationalError's id.
	state func() (state, errorID uint8)
	// set moves OperationalState.
	set func(state uint8) error
	// complete emits OperationCompletion with NoError.
	complete func() error
}

// The OperationalState values the cycle uses, the same in every
// derivation.
const (
	cycleStopped = uint8(opdef.OperationalStateStopped)
	cycleRunning = uint8(opdef.OperationalStateRunning)
)

// handle runs cmd and returns the ErrorStateEnum the response reports.
func (c cycle) handle(cmd uint32) (uint8, error) {
	state, errorID := c.state()
	switch cmd {
	case opdef.CmdStart:
		if errorID != uint8(opdef.ErrorStateNoError) {
			return uint8(opdef.ErrorStateUnableToStartOrResume), nil
		}
		if err := c.set(cycleRunning); err != nil {
			return 0, err
		}
	case opdef.CmdStop:
		if err := c.set(cycleStopped); err != nil {
			return 0, err
		}
		if state == cycleRunning {
			if err := c.complete(); err != nil {
				return 0, err
			}
		}
	default:
		return uint8(opdef.ErrorStateCommandInvalidInState), nil
	}
	slog.Info("appliance.command", slog.String("device", c.name), slog.Int("command", int(cmd)))
	return uint8(opdef.ErrorStateNoError), nil
}

// opCycle is a cycle on an OperationalState server.
type opCycle struct{ cycle }

// HandleOperationalCommand implements [opdef.CommandHandler].
func (c opCycle) HandleOperationalCommand(_ context.Context, cmd uint32) (opdef.ErrorStateStruct, error) {
	id, err := c.handle(cmd)
	return opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateEnum(id)}, err
}

// opCycleOn is the cycle of the OperationalState server *srv points to
// once the device is built.
func opCycleOn(name string, dev **device.Device, srv **opdef.Server) opCycle {
	return opCycle{cycle{
		name: name,
		state: func() (uint8, uint8) {
			st := (*srv).Get()
			return uint8(st.OperationalState), uint8(st.OperationalError.ErrorStateId)
		},
		set: func(state uint8) error {
			st := opdef.OperationalStateEnum(state)
			return (*srv).Set(opdef.Patch{OperationalState: &st})
		},
		complete: func() error {
			return (*srv).EmitOperationCompletion(endpointOf(*dev), opdef.OperationCompletionEvent{CompletionErrorCode: uint8(opdef.ErrorStateNoError)})
		},
	}}
}

// --- device: a dishwasher ---------------------------------------------------

// The dishwasher's alarms: the four it can detect, of which a blocked
// inflow and a blocked drain latch until reset.
const (
	dishwasherAlarms = dwadef.AlarmInflowError | dwadef.AlarmDrainError | dwadef.AlarmDoorError | dwadef.AlarmTempTooHigh
	dishwasherLatch  = dwadef.AlarmInflowError | dwadef.AlarmDrainError
)

// demoDishwasher is a dishwasher: OperationalState runs the programme, and
// DishwasherAlarm reports its faults (with Reset and ModifyEnabledAlarms).
type demoDishwasher struct {
	name string
	dev  *device.Device
	ops  *opdef.Server
}

var _ dwadef.Delegate = (*demoDishwasher)(nil)

func newDemoDishwasher(name string) *demoDishwasher { return &demoDishwasher{name: name} }

// device builds the dishwasher.
func (d *demoDishwasher) device(opts device.Options) (*device.Dishwasher, error) {
	stopped := opdef.OperationalStateStopped
	supported, latch, mask := dishwasherAlarms, dishwasherLatch, dishwasherAlarms
	dev, err := device.NewDishwasher(opts,
		opdef.Declare{
			CommandHandler: opCycleOn(d.name, &d.dev, &d.ops),
			Commands:       []uint32{opdef.CmdStart, opdef.CmdStop},
			Initial:        opdef.Patch{OperationalStateList: cycleStates(), OperationalState: &stopped},
		},
		dwadef.Declare{
			Features: dwadef.FeatureReset, Delegate: d, Commands: []uint32{dwadef.CmdModifyEnabledAlarms},
			Initial: dwadef.Patch{Supported: &supported, Latch: &latch, Mask: &mask},
		})
	if err != nil {
		return nil, err
	}
	d.dev, d.ops = dev.Device, dev.OperationalState()
	return dev, nil
}

// ModifyEnabledAlarms implements [dwadef.Delegate]: the dishwasher
// monitors whatever the controller enables.
func (d *demoDishwasher) ModifyEnabledAlarms(_ context.Context, mask uint32) error {
	slog.Info("dishwasher.alarm_mask", slog.String("device", d.name), slog.Uint64("mask", uint64(mask)))
	return nil
}

// ResetAlarms implements [dwadef.Delegate]: the dishwasher clears the
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

func newDemoOven(name string) *demoOven {
	return &demoOven{name: name, cavity: &demoOvenCavity{name: name + " cavity"}}
}

// device builds the oven — the Oven device type mandates no application
// cluster of its own — with its cavity as a part: a
// TemperatureControlledCabinet stating the Heater condition.
func (o *demoOven) device(opts device.Options) (*device.Oven, error) {
	oven, err := device.NewOven(opts)
	if err != nil {
		return nil, err
	}
	cavity, err := device.NewTemperatureControlledCabinet(partOf(opts, "demo:oven:cavity", conditionHeater), o.cavity.declarations()...)
	if err != nil {
		return nil, err
	}
	o.cavity.bind(cavity.Device)
	if err := oven.AddPart(cavity.Device); err != nil {
		return nil, err
	}
	return oven, nil
}

// demoOvenCavity is the oven's heating compartment.
type demoOvenCavity struct {
	name string

	mu  sync.Mutex
	dev *device.Device
	ops *ovendef.Server
}

var (
	_ ovenmdef.ModeChanger    = (*demoOvenCavity)(nil)
	_ tcdef.TemperatureSetter = (*demoOvenCavity)(nil)
)

// ovenCavityStates are the oven cavity's OperationalStateList: the four
// states of CHIP's all-clusters oven (oven-operational-state-delegate.h:37-40
// at the harness pin), Paused included although the cavity offers no Pause
// command — TC-OVENOPSTATE-2.2 step 3 requires every OperationalStateEnum
// value in the list. matter.js's OvenCavityOperationalStateServer requires
// only the Error entry (#assertOperationalStateList).
func ovenCavityStates() *[]ovendef.OperationalStateStruct {
	return &[]ovendef.OperationalStateStruct{
		{OperationalStateId: ovendef.OperationalStateStopped},
		{OperationalStateId: ovendef.OperationalStateRunning},
		{OperationalStateId: ovendef.OperationalStateError},
		{OperationalStateId: ovendef.OperationalStatePaused},
	}
}

// declarations declare the cavity's clusters.
func (c *demoOvenCavity) declarations() []device.Option {
	mode := func(label string, m uint8, tag ovenmdef.ModeTag) ovenmdef.ModeOptionStruct {
		return ovenmdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []ovenmdef.ModeTagStruct{{Value: tag}}}
	}
	modes := []ovenmdef.ModeOptionStruct{mode("Bake", ovenBake, ovenmdef.ModeTagBake), mode("Convection", ovenConvection, ovenmdef.ModeTagConvection)}
	bake, stopped, phase := ovenBake, ovendef.OperationalStateStopped, uint8(0)
	phases := ovenCavityPhases
	lo, hi, setpoint := ovenMin, ovenMax, ovenSetpoint
	return []device.Option{
		ovenmdef.Declare{ModeChanger: c, Initial: ovenmdef.Patch{SupportedModes: &modes, CurrentMode: &bake}},
		ovendef.Declare{
			CommandHandler: ovenCycle{c.cycle()},
			Commands:       []uint32{ovendef.CmdStart, ovendef.CmdStop},
			Initial: ovendef.Patch{
				OperationalStateList: ovenCavityStates(), OperationalState: &stopped, PhaseList: &phases, CurrentPhase: &phase,
			},
		},
		tcdef.Declare{
			Features: tcdef.FeatureTemperatureNumber, TemperatureSetter: c,
			Initial: tcdef.Patch{MinTemperature: &lo, MaxTemperature: &hi, TemperatureSetpoint: &setpoint},
		},
	}
}

// bind keeps the cavity's device for its commands and reports.
func (c *demoOvenCavity) bind(d *device.Device) {
	c.mu.Lock()
	c.dev, c.ops = d, ovendef.WrapOn(d.Server(ovendef.ClusterID), nil)
	c.mu.Unlock()
}

// server is the cavity's OvenCavityOperationalState; nil before the device
// is built.
func (c *demoOvenCavity) server() *ovendef.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ops
}

// cycle is the cavity's Start / Stop.
func (c *demoOvenCavity) cycle() cycle {
	return cycle{
		name: c.name,
		state: func() (uint8, uint8) {
			st := c.server().Get()
			return uint8(st.OperationalState), uint8(st.OperationalError.ErrorStateId)
		},
		set: func(state uint8) error { return c.setState(ovendef.OperationalStateEnum(state)) },
		complete: func() error {
			c.mu.Lock()
			dev := c.dev
			c.mu.Unlock()
			return c.server().EmitOperationCompletion(endpointOf(dev), ovendef.OperationCompletionEvent{CompletionErrorCode: uint8(ovendef.ErrorStateNoError)})
		},
	}
}

// setState moves the cavity's OperationalState, as a command or as the
// device's own report (the app pipe's OperationalStateChange).
func (c *demoOvenCavity) setState(st ovendef.OperationalStateEnum) error {
	srv := c.server()
	if srv == nil {
		return errNotBuilt
	}
	return srv.Set(ovendef.Patch{OperationalState: &st})
}

// setError sets the cavity's OperationalError (the app pipe's OnFault).
func (c *demoOvenCavity) setError(id ovendef.ErrorStateEnum) error {
	srv := c.server()
	if srv == nil {
		return errNotBuilt
	}
	return srv.Set(ovendef.Patch{OperationalError: &ovendef.ErrorStateStruct{ErrorStateId: id}})
}

// ovenCycle is a cycle on an OvenCavityOperationalState server.
type ovenCycle struct{ cycle }

// HandleOperationalCommand implements [ovendef.CommandHandler].
func (c ovenCycle) HandleOperationalCommand(_ context.Context, cmd uint32) (ovendef.ErrorStateStruct, error) {
	id, err := c.handle(cmd)
	return ovendef.ErrorStateStruct{ErrorStateId: ovendef.ErrorStateEnum(id)}, err
}

// ChangeToMode implements [ovenmdef.ModeChanger].
func (c *demoOvenCavity) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	slog.Info("oven.mode", slog.String("device", c.name), slog.Int("mode", int(newMode)))
	return uint8(ovenmdef.ModeChangeStatusSuccess), "", nil
}

// SetTemperature implements [tcdef.TemperatureSetter].
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

// microwaveMaxCookTime is the longest cook time the microwave takes, in s;
// microwaveMaxPower the power it starts at (MicrowaveOvenControl's default
// MaxPower, 100 %).
const (
	microwaveMaxCookTime uint32 = 3600
	microwaveMaxPower    uint8  = 100
)

// demoMicrowave is a microwave oven: OperationalState runs it,
// MicrowaveOvenMode picks Normal or Defrost, MicrowaveOvenControl sets
// cook time and power.
type demoMicrowave struct {
	name string

	mu  sync.Mutex
	dev *device.MicrowaveOven
}

var (
	_ mwocdef.Oven         = (*demoMicrowave)(nil)
	_ opdef.CommandHandler = (*demoMicrowave)(nil)
)

func newDemoMicrowave(name string) *demoMicrowave { return &demoMicrowave{name: name} }

// device builds the microwave.
func (m *demoMicrowave) device(opts device.Options) (*device.MicrowaveOven, error) {
	states := []opdef.OperationalStateStruct{
		{OperationalStateId: opdef.OperationalStateStopped},
		{OperationalStateId: opdef.OperationalStateRunning},
		{OperationalStateId: opdef.OperationalStatePaused},
		{OperationalStateId: opdef.OperationalStateError},
	}
	stopped := opdef.OperationalStateStopped
	mode := func(label string, m uint8, tag mwomdef.ModeTag) mwomdef.ModeOptionStruct {
		return mwomdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []mwomdef.ModeTagStruct{{Value: tag}}}
	}
	modes := []mwomdef.ModeOptionStruct{mode("Normal", microwaveNormal, mwomdef.ModeTagNormal), mode("Defrost", microwaveDefrost, mwomdef.ModeTagDefrost)}
	normal, maxCook, power := microwaveNormal, microwaveMaxCookTime, microwaveMaxPower
	d, err := device.NewMicrowaveOven(opts,
		opdef.Declare{
			CommandHandler: m,
			Commands:       []uint32{opdef.CmdPause, opdef.CmdResume, opdef.CmdStart, opdef.CmdStop},
			Initial:        opdef.Patch{OperationalStateList: &states, OperationalState: &stopped},
		},
		mwomdef.Declare{Initial: mwomdef.Patch{SupportedModes: &modes, CurrentMode: &normal}},
		mwocdef.Declare{
			Features: mwocdef.FeaturePowerAsNumber, Oven: m, Commands: []uint32{mwocdef.CmdAddMoreTime},
			Initial: mwocdef.Patch{MaxCookTime: &maxCook, PowerSetting: &power},
		})
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.dev = d
	m.mu.Unlock()
	return d, nil
}

// oven is the microwave's device.
func (m *demoMicrowave) oven() *device.MicrowaveOven {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dev
}

// OperationalState implements [mwocdef.Oven].
func (m *demoMicrowave) OperationalState() uint8 {
	return uint8(m.oven().OperationalState().Get().OperationalState)
}

// NormalMode implements [mwocdef.Oven]: the mode tagged Normal.
func (m *demoMicrowave) NormalMode() (uint8, bool) { return microwaveNormal, true }

// SupportedMode implements [mwocdef.Oven].
func (m *demoMicrowave) SupportedMode(mode uint8) bool {
	return mode == microwaveNormal || mode == microwaveDefrost
}

// StartSupported implements [mwocdef.Oven]: the OperationalState server
// accepts Start.
func (m *demoMicrowave) StartSupported() bool { return true }

// SetCookingParameters implements [mwocdef.Oven]: the microwave takes the
// mode and, when asked, starts.
func (m *demoMicrowave) SetCookingParameters(_ context.Context, mode uint8, cookTime uint32, start bool, power, _ *uint8) error {
	if err := m.oven().MicrowaveOvenMode().Set(mwomdef.Patch{CurrentMode: &mode}); err != nil {
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

// ModifyCookTime implements [mwocdef.Oven].
func (m *demoMicrowave) ModifyCookTime(_ context.Context, cookTime uint32) error {
	slog.Info("microwave.cook_time", slog.String("device", m.name), slog.Uint64("cook_time", uint64(cookTime)))
	if m.OperationalState() == uint8(opdef.OperationalStateRunning) {
		return m.oven().OperationalState().Set(opdef.Patch{CountdownTime: &cookTime})
	}
	return nil
}

// run starts cooking for cookTime seconds.
func (m *demoMicrowave) run(cookTime uint32) error {
	running := opdef.OperationalStateRunning
	return m.oven().OperationalState().Set(opdef.Patch{OperationalState: &running, CountdownTime: &cookTime})
}

// HandleOperationalCommand implements [opdef.CommandHandler].
func (m *demoMicrowave) HandleOperationalCommand(_ context.Context, cmd uint32) (opdef.ErrorStateStruct, error) {
	ops := m.oven().OperationalState()
	set := func(st opdef.OperationalStateEnum) error { return ops.Set(opdef.Patch{OperationalState: &st}) }
	var err error
	switch cmd {
	case opdef.CmdStart:
		err = m.run(m.oven().MicrowaveOvenControl().Get().CookTime)
	case opdef.CmdResume:
		err = set(opdef.OperationalStateRunning)
	case opdef.CmdPause:
		err = set(opdef.OperationalStatePaused)
	case opdef.CmdStop:
		running := m.OperationalState() != uint8(opdef.OperationalStateStopped)
		stopped := opdef.OperationalStateStopped
		err = ops.Set(opdef.Patch{OperationalState: &stopped, Null: []uint32{opdef.AttrCountdownTime}})
		if err == nil && running {
			err = ops.EmitOperationCompletion(endpointOf(m.oven().Device), opdef.OperationCompletionEvent{CompletionErrorCode: uint8(opdef.ErrorStateNoError)})
		}
	default:
		return opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateCommandInvalidInState}, nil
	}
	if err != nil {
		return opdef.ErrorStateStruct{}, err
	}
	slog.Info("microwave.command", slog.String("device", m.name), slog.Int("command", int(cmd)))
	return opdef.ErrorStateStruct{}, nil
}

// --- device: a laundry dryer ------------------------------------------------

// demoDryer is a laundry dryer: OperationalState runs the cycle,
// LaundryDryerControls picks how dry.
type demoDryer struct {
	name string
	dev  *device.Device
	ops  *opdef.Server
}

func newDemoDryer(name string) *demoDryer { return &demoDryer{name: name} }

// device builds the dryer: Low, Normal and Extra dryness, Normal selected.
func (d *demoDryer) device(opts device.Options) (*device.LaundryDryer, error) {
	stopped := opdef.OperationalStateStopped
	levels := []ldcdef.DrynessLevelEnum{ldcdef.DrynessLevelLow, ldcdef.DrynessLevelNormal, ldcdef.DrynessLevelExtra}
	normal := ldcdef.DrynessLevelNormal
	dev, err := device.NewLaundryDryer(opts,
		opdef.Declare{
			CommandHandler: opCycleOn(d.name, &d.dev, &d.ops),
			Commands:       []uint32{opdef.CmdStart, opdef.CmdStop},
			Initial:        opdef.Patch{OperationalStateList: cycleStates(), OperationalState: &stopped},
		},
		ldcdef.Declare{
			Initial: ldcdef.Patch{SupportedDrynessLevels: &levels, SelectedDrynessLevel: &normal},
			// The dryer takes every level the server lets through.
			OnWrite: func(_ context.Context, next ldcdef.State, _ uint32) error {
				v := -1
				if next.SelectedDrynessLevel != nil {
					v = int(*next.SelectedDrynessLevel)
				}
				slog.Info("dryer.dryness", slog.String("device", d.name), slog.Int("level", v))
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	d.dev, d.ops = dev.Device, dev.OperationalState()
	return dev, nil
}
