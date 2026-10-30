// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lwcdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	rfadef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	rtccdef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	rvccdef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcopdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rvcrdef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/device"
)

// The two appliances below run a programme: the device kit's
// OperationalState (or RvcOperationalState) server reports where in it
// they are, and its ModeBase server selects which programme. Both servers
// keep their state themselves, as a matter.js behavior does, and fire their
// own attribute changes; the appliance is the servers' port and moves the
// state through the device's typed accessors.
//
// What the model does and does not claim: a cycle never ends on its own.
// Completion, errors and the RVC reaching its dock are things the device
// reports, and in this example the debug control hook (control.go) reports
// them on a test's behalf — the same seam a real host drives from its
// southbound event stream.

// errNotBuilt reports an appliance whose device is not built yet.
var errNotBuilt = errors.New("the device is not built yet")

// endpointOf is the endpoint number d is served under; 0 before the node
// numbered it (an event emitted then has no emitter to reach anyway).
func endpointOf(d *device.Device) uint16 {
	n, _ := d.EndpointNumber()
	return n
}

// elapsed is an OperationCompletion time in whole seconds.
func elapsed(d time.Duration) *spec.Nullable[uint32] {
	return &spec.Nullable[uint32]{Value: uint32(d.Round(time.Second) / time.Second)} //nolint:gosec // an appliance cycle's seconds fit
}

// --- device: a laundry washer ---------------------------------------------

// Laundry-washer programmes (LaundryWasherMode SupportedModes). The numbers
// are this host's own; the tags are the derived cluster's standard ones
// (matter.js laundry-washer-mode.element.ts).
const (
	washNormal   uint8 = 0
	washDelicate uint8 = 1
	washHeavy    uint8 = 2
	// washLocked is a programme the washer refuses while a cycle runs —
	// the host-refused path ChangeToMode reports with InvalidInMode.
	washLocked uint8 = 3
)

// demoWasher is a laundry washer: LaundryWasherMode picks the programme,
// OperationalState runs it, LaundryWasherControls sets spin speed and
// rinses.
type demoWasher struct {
	name string

	mu  sync.Mutex
	dev *device.LaundryWasher
	ops *opdef.Server

	running bool
	// runStart, pausedAt and pausedFor time the current cycle for the
	// OperationCompletion a Stop emits.
	runStart, pausedAt time.Time
	pausedFor          time.Duration
	// startBlocked is set by a reported fault: Start and Resume answer
	// UnableToStartOrResume until a fault of NoError clears it.
	startBlocked bool
	// remaining is the cycle's CountdownTime in seconds; ticking is the
	// stop channel of the goroutine counting it down while the cycle runs.
	remaining uint32
	ticking   chan struct{}
}

// washCycleSeconds is the countdown a started cycle begins with. Short,
// because TC-OPSTATE-2.5 waits out the whole countdown inside its 90 s
// budget before it stops the cycle.
const washCycleSeconds = 30

var (
	_ opdef.CommandHandler = (*demoWasher)(nil)
	_ lwmdef.ModeChanger   = (*demoWasher)(nil)
)

func newDemoWasher(name string) *demoWasher { return &demoWasher{name: name} }

// device builds the washer: OperationalState with the four commands, the
// three phases, CountdownTime and OperationCompletion; the four
// programmes; spin speeds and rinses.
func (w *demoWasher) device(opts device.Options) (*device.LaundryWasher, error) {
	states := []opdef.OperationalStateStruct{
		{OperationalStateId: opdef.OperationalStateStopped},
		{OperationalStateId: opdef.OperationalStateRunning},
		{OperationalStateId: opdef.OperationalStatePaused},
		{OperationalStateId: opdef.OperationalStateError},
	}
	stopped, phases, phase := opdef.OperationalStateStopped, []string{"wash", "rinse", "spin"}, uint8(0)
	mode := func(label string, m uint8, tag lwmdef.ModeTag) lwmdef.ModeOptionStruct {
		return lwmdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []lwmdef.ModeTagStruct{{Value: tag}}}
	}
	modes := []lwmdef.ModeOptionStruct{
		mode("Normal", washNormal, lwmdef.ModeTagNormal), mode("Delicate", washDelicate, lwmdef.ModeTagDelicate),
		mode("Heavy", washHeavy, lwmdef.ModeTagHeavy), mode("Locked while running", washLocked, lwmdef.ModeTagQuiet),
	}
	current := washNormal
	speeds, spin := []string{"Off", "400", "800", "1200"}, uint8(2)
	rinses := []lwcdef.NumberOfRinsesEnum{lwcdef.NumberOfRinsesNone, lwcdef.NumberOfRinsesNormal, lwcdef.NumberOfRinsesExtra}
	rinse := lwcdef.NumberOfRinsesNormal
	d, err := device.NewLaundryWasher(opts,
		opdef.Declare{
			CommandHandler: w,
			Commands:       []uint32{opdef.CmdPause, opdef.CmdResume, opdef.CmdStart, opdef.CmdStop},
			Events:         []uint32{opdef.EventOperationCompletion},
			Initial: opdef.Patch{
				OperationalStateList: &states, OperationalState: &stopped, PhaseList: &phases, CurrentPhase: &phase,
				Null: []uint32{opdef.AttrCountdownTime},
			},
		},
		lwmdef.Declare{ModeChanger: w, Initial: lwmdef.Patch{SupportedModes: &modes, CurrentMode: &current}},
		lwcdef.Declare{
			Features: lwcdef.FeatureSpin | lwcdef.FeatureRinse,
			Initial:  lwcdef.Patch{SpinSpeeds: &speeds, SpinSpeedCurrent: &spin, SupportedRinses: &rinses, NumberOfRinses: &rinse},
			// The washer takes every spin speed and rinse count the server
			// lets through.
			OnWrite: func(_ context.Context, next lwcdef.State, attrID uint32) error {
				switch attrID {
				case lwcdef.AttrSpinSpeedCurrent:
					v := -1
					if next.SpinSpeedCurrent != nil {
						v = int(*next.SpinSpeedCurrent)
					}
					slog.Info("washer.spin", slog.String("device", w.name), slog.Int("spin", v))
				case lwcdef.AttrNumberOfRinses:
					slog.Info("washer.rinses", slog.String("device", w.name), slog.Int("rinses", int(next.NumberOfRinses)))
				}
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.dev, w.ops = d, d.OperationalState()
	w.mu.Unlock()
	return d, nil
}

// server is the washer's OperationalState; nil before the device is built.
func (w *demoWasher) server() *opdef.Server {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ops
}

// setCountdown sets CountdownTime; nil is null.
func (w *demoWasher) setCountdown(seconds *uint32) error {
	if seconds == nil {
		return w.server().Set(opdef.Patch{Null: []uint32{opdef.AttrCountdownTime}})
	}
	return w.server().Set(opdef.Patch{CountdownTime: seconds})
}

// HandleOperationalCommand implements [opdef.CommandHandler]. The server
// has already refused what is invalid in the current state; what arrives
// here moves the cycle.
func (w *demoWasher) HandleOperationalCommand(_ context.Context, cmd uint32) (opdef.ErrorStateStruct, error) {
	var next opdef.OperationalStateEnum
	switch cmd {
	case opdef.CmdStart, opdef.CmdResume:
		next = opdef.OperationalStateRunning
	case opdef.CmdPause:
		next = opdef.OperationalStatePaused
	case opdef.CmdStop:
		next = opdef.OperationalStateStopped
	default:
		return opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateCommandInvalidInState}, nil
	}
	w.mu.Lock()
	if w.startBlocked && (cmd == opdef.CmdStart || cmd == opdef.CmdResume) {
		// A fault that keeps the cycle from starting — matter.js
		// TestOperationalStateServer start()/resume() with startBlocked.
		w.mu.Unlock()
		return opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateUnableToStartOrResume}, nil
	}
	wasRunning := w.running
	now := time.Now()
	var completion *opdef.OperationCompletionEvent
	switch {
	case cmd == opdef.CmdStart:
		w.runStart, w.pausedFor, w.pausedAt = now, 0, time.Time{}
	case cmd == opdef.CmdPause && w.pausedAt.IsZero():
		w.pausedAt = now
	case cmd == opdef.CmdResume && !w.pausedAt.IsZero():
		w.pausedFor += now.Sub(w.pausedAt)
		w.pausedAt = time.Time{}
	case cmd == opdef.CmdStop && wasRunning:
		// Stopping a cycle ends the operation: OperationCompletion with
		// the time it lasted and the time it was paused, which
		// TC-OPSTATE-2.5 holds against the countdown it waited out.
		paused := w.pausedFor
		if !w.pausedAt.IsZero() {
			paused += now.Sub(w.pausedAt)
		}
		// TotalOperationalTime is the whole operation, pauses included;
		// TC-OPSTATE-2.5 expects 1.5 x the countdown after a pause of half
		// of it.
		completion = &opdef.OperationCompletionEvent{
			CompletionErrorCode:  uint8(opdef.ErrorStateNoError),
			TotalOperationalTime: elapsed(now.Sub(w.runStart)),
			PausedTime:           elapsed(paused),
		}
		w.pausedAt = time.Time{}
	}
	w.running = next == opdef.OperationalStateRunning || next == opdef.OperationalStatePaused
	if cmd == opdef.CmdStart || next == opdef.OperationalStateStopped {
		w.remaining = washCycleSeconds
	}
	dev := w.dev
	w.mu.Unlock()
	ops := w.server()
	if err := ops.Set(opdef.Patch{OperationalState: &next}); err != nil {
		return opdef.ErrorStateStruct{}, err
	}
	if completion != nil {
		if err := ops.EmitOperationCompletion(endpointOf(dev.Device), *completion); err != nil {
			return opdef.ErrorStateStruct{}, err
		}
	}
	// A running cycle counts its time down, a paused one holds it, a
	// stopped one has none — what TC-OPSTATE-2.2 reads after a Start.
	switch next {
	case opdef.OperationalStateRunning:
		w.mu.Lock()
		remaining := w.remaining
		w.mu.Unlock()
		_ = w.setCountdown(&remaining)
		w.startCountdown()
	case opdef.OperationalStatePaused:
		w.stopCountdown()
	default:
		w.stopCountdown()
		_ = w.setCountdown(nil)
	}
	slog.Info("washer.state", slog.String("device", w.name), slog.Int("state", int(next)))
	return opdef.ErrorStateStruct{}, nil
}

// startCountdown counts CountdownTime down once a second until the cycle
// stops or pauses; it does nothing when already counting.
func (w *demoWasher) startCountdown() {
	w.mu.Lock()
	if w.ticking != nil {
		w.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	w.ticking = stop
	w.mu.Unlock()
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				w.mu.Lock()
				if w.remaining > 0 {
					w.remaining--
				}
				remaining := w.remaining
				w.mu.Unlock()
				_ = w.setCountdown(&remaining)
			}
		}
	}()
}

// stopCountdown ends the countdown goroutine, if any.
func (w *demoWasher) stopCountdown() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ticking != nil {
		close(w.ticking)
		w.ticking = nil
	}
}

// ChangeToMode implements [lwmdef.ModeChanger]. The locked programme is
// refused while a cycle runs or is paused — the device decides, and says why.
func (w *demoWasher) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	if newMode == washLocked && running {
		return uint8(lwmdef.ModeChangeStatusInvalidInMode), "not while a cycle runs", nil
	}
	slog.Info("washer.mode", slog.String("device", w.name), slog.Int("mode", int(newMode)))
	return uint8(lwmdef.ModeChangeStatusSuccess), "", nil
}

// reportError is the device reporting a fault: OperationalState goes to
// Error and the OperationalError event fires (the server does both).
func (w *demoWasher) reportError(id opdef.ErrorStateEnum) error {
	ops := w.server()
	if ops == nil {
		return errNotBuilt
	}
	w.mu.Lock()
	w.running = false
	// Any fault blocks a Start until the fault is cleared (matter.js
	// AllClustersTestInstance.ts OnFault: startBlocked for 1 and 2).
	w.startBlocked = id != opdef.ErrorStateNoError
	w.mu.Unlock()
	slog.Info("washer.error", slog.String("device", w.name), slog.Int("error_state", int(id)))
	return ops.Set(opdef.Patch{OperationalError: &opdef.ErrorStateStruct{ErrorStateId: id}})
}

// reportState is the device moving its own cycle (the app pipe's
// OperationalStateChange).
func (w *demoWasher) reportState(st opdef.OperationalStateEnum) error {
	ops := w.server()
	if ops == nil {
		return errNotBuilt
	}
	return ops.Set(opdef.Patch{OperationalState: &st})
}

// reportCompletion is the device finishing its cycle: the OperationCompletion
// event, then Stopped with no countdown.
func (w *demoWasher) reportCompletion() error {
	ops := w.server()
	if ops == nil {
		return errNotBuilt
	}
	w.stopCountdown()
	w.mu.Lock()
	w.running = false
	dev := w.dev
	w.mu.Unlock()
	if err := ops.EmitOperationCompletion(endpointOf(dev.Device), opdef.OperationCompletionEvent{CompletionErrorCode: uint8(opdef.ErrorStateNoError)}); err != nil {
		return err
	}
	if err := ops.Set(opdef.Patch{OperationalError: &opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateNoError}}); err != nil {
		return err
	}
	_ = w.setCountdown(nil)
	slog.Info("washer.complete", slog.String("device", w.name))
	stopped := opdef.OperationalStateStopped
	return ops.Set(opdef.Patch{OperationalState: &stopped})
}

// --- device: a robotic vacuum cleaner ---------------------------------------

// RVC run and clean modes. RvcRunMode needs an Idle mode and a Cleaning
// mode (rvc-run-mode.element.ts); RvcCleanMode at least one cleaning type.
const (
	rvcIdle     uint8 = 0
	rvcCleaning uint8 = 1
	rvcMapping  uint8 = 2

	rvcVacuum uint8 = 0
	rvcMop    uint8 = 1
)

// demoVacuum is a robotic vacuum cleaner: RvcRunMode starts and ends a run,
// RvcCleanMode picks vacuum or mop, RvcOperationalState reports it.
//
// The run mode drives the operational state, as on a real RVC: changing to
// a cleaning run mode starts a run (Running), changing back to Idle stops it
// (Stopped). GoHome sends it to the dock (SeekingCharger); arriving there
// is a device report (control.go).
type demoVacuum struct {
	name string

	mu      sync.Mutex
	dev     *device.RoboticVacuumCleaner
	runMode uint8
	paused  bool
}

var _ rvcopdef.CommandHandler = (*demoVacuum)(nil)

func newDemoVacuum(name string) *demoVacuum { return &demoVacuum{name: name} }

// declarations declare the robot's RvcOperationalState — Pause, Resume and
// GoHome; matter.js's RVC test node starts Stopped, in run mode Idle
// (support/chip-testing/src/RvcTestInstance.ts) — and its two modes.
func (v *demoVacuum) declarations() []device.Option {
	state := func(id rvcopdef.OperationalStateEnum) rvcopdef.OperationalStateStruct {
		return rvcopdef.OperationalStateStruct{OperationalStateId: id}
	}
	states := []rvcopdef.OperationalStateStruct{
		state(rvcopdef.OperationalStateStopped), state(rvcopdef.OperationalStateRunning), state(rvcopdef.OperationalStatePaused),
		state(rvcopdef.OperationalStateError), state(rvcopdef.OperationalStateSeekingCharger),
		state(rvcopdef.OperationalStateCharging), state(rvcopdef.OperationalStateDocked),
	}
	stopped := rvcopdef.OperationalStateStopped
	run := func(label string, m uint8, tag rvcrdef.ModeTag) rvcrdef.ModeOptionStruct {
		return rvcrdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []rvcrdef.ModeTagStruct{{Value: tag}}}
	}
	runModes := []rvcrdef.ModeOptionStruct{
		run("Idle", rvcIdle, rvcrdef.ModeTagIdle), run("Cleaning", rvcCleaning, rvcrdef.ModeTagCleaning),
		run("Mapping", rvcMapping, rvcrdef.ModeTagMapping),
	}
	clean := func(label string, m uint8, tag rvccdef.ModeTag) rvccdef.ModeOptionStruct {
		return rvccdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []rvccdef.ModeTagStruct{{Value: tag}}}
	}
	cleanModes := []rvccdef.ModeOptionStruct{clean("Vacuum", rvcVacuum, rvccdef.ModeTagVacuum), clean("Mop", rvcMop, rvccdef.ModeTagMop)}
	idle, vacuum := rvcIdle, rvcVacuum
	return []device.Option{
		rvcopdef.Declare{
			CommandHandler: v,
			Commands:       []uint32{rvcopdef.CmdPause, rvcopdef.CmdResume, rvcopdef.CmdGoHome},
			Initial:        rvcopdef.Patch{OperationalStateList: &states, OperationalState: &stopped},
		},
		rvcrdef.Declare{ModeChanger: rvcRunChanger{v}, Initial: rvcrdef.Patch{SupportedModes: &runModes, CurrentMode: &idle}},
		rvccdef.Declare{ModeChanger: rvcCleanChanger{v}, Initial: rvccdef.Patch{SupportedModes: &cleanModes, CurrentMode: &vacuum}},
	}
}

// bind keeps the robot's device for its reports.
func (v *demoVacuum) bind(d *device.RoboticVacuumCleaner) {
	v.mu.Lock()
	v.dev = d
	v.mu.Unlock()
}

// device is the robot's device; nil before it is built.
func (v *demoVacuum) device() *device.RoboticVacuumCleaner {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dev
}

// state is the robot's current OperationalState.
func (v *demoVacuum) state() rvcopdef.OperationalStateEnum {
	return v.device().RvcOperationalState().Get().OperationalState
}

// setState moves the robot's OperationalState.
func (v *demoVacuum) setState(st rvcopdef.OperationalStateEnum) error {
	return v.device().RvcOperationalState().Set(rvcopdef.Patch{OperationalState: &st})
}

// HandleOperationalCommand implements [rvcopdef.CommandHandler], as
// matter.js's RVC test node does (TestRvcOperationalStateServer): Resume
// is refused on the dock (Charging, Docked) and GoHome in Error, with
// CommandInvalidInState.
func (v *demoVacuum) HandleOperationalCommand(_ context.Context, cmd uint32) (rvcopdef.ErrorStateStruct, error) {
	var next rvcopdef.OperationalStateEnum
	current := v.state()
	invalid := rvcopdef.ErrorStateStruct{ErrorStateId: rvcopdef.ErrorStateCommandInvalidInState}
	switch cmd {
	case rvcopdef.CmdPause:
		next = rvcopdef.OperationalStatePaused
	case rvcopdef.CmdResume:
		if current == rvcopdef.OperationalStateCharging || current == rvcopdef.OperationalStateDocked {
			return invalid, nil
		}
		next = rvcopdef.OperationalStateRunning
	case rvcopdef.CmdGoHome:
		if current == rvcopdef.OperationalStateError {
			return invalid, nil
		}
		next = rvcopdef.OperationalStateSeekingCharger
	default:
		return invalid, nil
	}
	v.mu.Lock()
	v.paused = next == rvcopdef.OperationalStatePaused
	v.mu.Unlock()
	slog.Info("vacuum.state", slog.String("device", v.name), slog.Int("state", int(next)))
	if err := v.setState(next); err != nil {
		return rvcopdef.ErrorStateStruct{}, err
	}
	return rvcopdef.ErrorStateStruct{}, nil
}

// rvcRunChanger applies RvcRunMode changes as matter.js's RVC test node
// does (TestRvcRunModeServer.changeToMode): Idle → Cleaning starts a run
// (Running), Cleaning → Idle sends the robot home (SeekingCharger), and
// Mapping cannot be entered while cleaning (InvalidInMode).
type rvcRunChanger struct{ v *demoVacuum }

func (c rvcRunChanger) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	v := c.v
	v.mu.Lock()
	current := v.runMode
	v.mu.Unlock()
	var next *rvcopdef.OperationalStateEnum
	switch {
	case newMode == rvcIdle && current == rvcCleaning:
		st := rvcopdef.OperationalStateSeekingCharger
		next = &st
	case newMode == rvcCleaning && current == rvcIdle:
		st := rvcopdef.OperationalStateRunning
		next = &st
	}
	// Cleaning ↔ Mapping never arrives: without DIRECTMODECH the RvcRunMode
	// server answers InvalidInMode for it before asking.
	v.mu.Lock()
	v.runMode = newMode
	v.paused = false
	v.mu.Unlock()
	slog.Info("vacuum.run_mode", slog.String("device", v.name), slog.Int("mode", int(newMode)))
	if next != nil {
		if err := v.setState(*next); err != nil {
			return uint8(rvcrdef.ModeChangeStatusGenericFailure), "", err
		}
	}
	if current == rvcCleaning && newMode == rvcIdle {
		// The cleaning run is over: OperationCompletion, which the
		// RoboticVacuumCleaner device type mandates (its element
		// requirement on RvcOperationalState; matter.js's RVC test node
		// enables the event, TestRvcOperationalStateServer.ts).
		d := v.device()
		if err := d.RvcOperationalState().EmitOperationCompletion(endpointOf(d.Device),
			rvcopdef.OperationCompletionEvent{CompletionErrorCode: uint8(rvcopdef.ErrorStateNoError)}); err != nil {
			return uint8(rvcrdef.ModeChangeStatusGenericFailure), "", err
		}
	}
	return uint8(rvcrdef.ModeChangeStatusSuccess), "", nil
}

// rvcCleanChanger applies RvcCleanMode changes. A change is refused with
// InvalidInMode outside the Idle run mode, as matter.js's RVC test node
// does (TestRvcCleanModeServer.changeToMode; TC-RVCCLEANM-2.2).
type rvcCleanChanger struct{ v *demoVacuum }

func (c rvcCleanChanger) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	v := c.v
	v.mu.Lock()
	running := v.runMode != rvcIdle
	v.mu.Unlock()
	if running {
		return uint8(rvccdef.ModeChangeStatusInvalidInMode), "can not switch operative modes while the device is operating", nil
	}
	slog.Info("vacuum.clean_mode", slog.String("device", v.name), slog.Int("mode", int(newMode)))
	return uint8(rvccdef.ModeChangeStatusSuccess), "", nil
}

// setRunMode sets the robot's RvcRunMode CurrentMode as the device's own
// report.
func (v *demoVacuum) setRunMode(mode uint8) error {
	v.mu.Lock()
	v.runMode = mode
	v.mu.Unlock()
	return v.device().RvcRunMode().Set(rvcrdef.Patch{CurrentMode: &mode})
}

// reportDocked is the robot arriving at its dock after GoHome.
func (v *demoVacuum) reportDocked() error {
	if v.device() == nil {
		return errNotBuilt
	}
	if err := v.setRunMode(rvcIdle); err != nil {
		return err
	}
	slog.Info("vacuum.docked", slog.String("device", v.name))
	// matter.js RvcTestInstance "docked": the robot charges on the dock.
	return v.setState(rvcopdef.OperationalStateCharging)
}

// reportError is the robot reporting a fault (stuck, dust bin full, …).
func (v *demoVacuum) reportError(id rvcopdef.ErrorStateEnum) error {
	d := v.device()
	if d == nil {
		return errNotBuilt
	}
	slog.Info("vacuum.error", slog.String("device", v.name), slog.Int("error_state", int(id)))
	return d.RvcOperationalState().Set(rvcopdef.Patch{OperationalError: &rvcopdef.ErrorStateStruct{ErrorStateId: id}})
}

// reset is the rvc-app's Reset: Stopped, run mode Idle, no error — matter.js
// RvcTestInstance "reset".
func (v *demoVacuum) reset() error {
	d := v.device()
	if d == nil {
		return errNotBuilt
	}
	v.mu.Lock()
	v.paused = false
	v.mu.Unlock()
	if err := v.setRunMode(rvcIdle); err != nil {
		return err
	}
	if err := d.RvcOperationalState().Set(rvcopdef.Patch{OperationalError: &rvcopdef.ErrorStateStruct{}}); err != nil {
		return err
	}
	return v.setState(rvcopdef.OperationalStateStopped)
}

// reportState is a dock report of the rvc-app's pipe — ChargerFound and
// Charging (Charging), Charged (Docked).
func (v *demoVacuum) reportState(st rvcopdef.OperationalStateEnum) error {
	if v.device() == nil {
		return errNotBuilt
	}
	slog.Info("vacuum.dock", slog.String("device", v.name), slog.Int("state", int(st)))
	return v.setState(st)
}

// --- device: a refrigerator ------------------------------------------------

// Device types of the refrigerator and its compartment (parity/schema.json):
// Refrigerator (0x0070) requires a TemperatureControlledCabinet (0x0071)
// part, and a descendant with the Cooler condition.
const (
	deviceTypeRefrigerator                 uint16 = 0x0070
	deviceTypeTemperatureControlledCabinet uint16 = 0x0071
)

// conditionCooler is the TemperatureControlledCabinet condition a cooling
// compartment states; with it the cabinet's
// RefrigeratorAndTemperatureControlledCabinetMode ("[Cooler]") is in
// scope, and the Refrigerator's descendant requirement is met.
const conditionCooler = "Cooler"

// Refrigerator modes (RefrigeratorAndTemperatureControlledCabinetMode
// SupportedModes). The numbers are this host's own; Auto is the tag the
// derivation requires.
const (
	fridgeAuto      uint8 = 0
	fridgeRapidCool uint8 = 1
)

// Cabinet temperatures, in 0.01 °C: a cooling compartment from 1 °C to
// 8 °C in half-degree steps.
const (
	cabinetMin      int16 = 100
	cabinetMax      int16 = 800
	cabinetStep     int16 = 50
	cabinetSetpoint int16 = 400
)

// demoFridge is a refrigerator: its own endpoint serves the appliance's
// RefrigeratorAndTemperatureControlledCabinetMode and RefrigeratorAlarm
// (the door), and its one cabinet — a part of its own — serves
// TemperatureControl and the cabinet's mode.
type demoFridge struct {
	name    string
	cabinet *demoCabinet

	mu    sync.Mutex
	alarm *rfadef.Server
}

// fridgeDoorOpen is the RefrigeratorAlarm the fridge supports.
const fridgeDoorOpen = rfadef.AlarmDoorOpen

var _ rtccdef.ModeChanger = (*demoFridge)(nil)

func newDemoFridge(name string) *demoFridge {
	return &demoFridge{name: name, cabinet: &demoCabinet{name: name + " cabinet"}}
}

// fridgeModes are the refrigerator's and the cabinet's modes.
func fridgeModes() *[]rtccdef.ModeOptionStruct {
	mode := func(label string, m uint8, tag rtccdef.ModeTag) rtccdef.ModeOptionStruct {
		return rtccdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []rtccdef.ModeTagStruct{{Value: tag}}}
	}
	return &[]rtccdef.ModeOptionStruct{mode("Auto", fridgeAuto, rtccdef.ModeTagAuto), mode("Rapid Cool", fridgeRapidCool, rtccdef.ModeTagRapidCool)}
}

// ChangeToMode implements [rtccdef.ModeChanger]: the appliance takes
// every supported mode.
func (r *demoFridge) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	slog.Info("fridge.mode", slog.String("device", r.name), slog.Int("mode", int(newMode)))
	return uint8(rtccdef.ModeChangeStatusSuccess), "", nil
}

// reportDoor is CHIP's SetRefrigeratorDoorStatus app-pipe command, as the
// all-clusters app applies it (examples/all-clusters-app/linux/
// AllClustersCommandDelegate.cpp SetRefrigeratorDoorStatusHandler at the
// harness pin): a closed door sets Mask to 0 (which clears State); an open
// one sets Mask and State to DoorOpen.
func (r *demoFridge) reportDoor(open bool) error {
	r.mu.Lock()
	alarm := r.alarm
	r.mu.Unlock()
	if alarm == nil {
		return errNotBuilt
	}
	slog.Info("fridge.door", slog.String("device", r.name), slog.Bool("open", open))
	if !open {
		var none rfadef.AlarmBitmap
		return alarm.Set(rfadef.Patch{Mask: &none})
	}
	mask, state := fridgeDoorOpen, fridgeDoorOpen
	return alarm.Set(rfadef.Patch{Mask: &mask, State: &state})
}

// device builds the fridge with its cooling compartment as a part: a
// TemperatureControlledCabinet stating the Cooler condition the
// Refrigerator requires of a descendant.
func (r *demoFridge) device(opts device.Options) (*device.Refrigerator, error) {
	auto, supported, mask := fridgeAuto, fridgeDoorOpen, fridgeDoorOpen
	fridge, err := device.NewRefrigerator(opts,
		rtccdef.Declare{ModeChanger: r, Initial: rtccdef.Patch{SupportedModes: fridgeModes(), CurrentMode: &auto}},
		rfadef.Declare{Initial: rfadef.Patch{Supported: &supported, Mask: &mask}})
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.alarm = rfadef.WrapOn(fridge.Server(rfadef.ClusterID), nil)
	r.mu.Unlock()
	cabinet, err := device.NewTemperatureControlledCabinet(partOf(opts, "demo:fridge:cabinet", conditionCooler), r.cabinet.declarations()...)
	if err != nil {
		return nil, err
	}
	if err := fridge.AddPart(cabinet.Device); err != nil {
		return nil, err
	}
	return fridge, nil
}

// demoCabinet is the refrigerator's cooling compartment:
// TemperatureControl with a numeric setpoint in steps (TN + STEP), and the
// compartment's own RefrigeratorAndTemperatureControlledCabinetMode.
type demoCabinet struct{ name string }

var (
	_ tcdef.TemperatureSetter = (*demoCabinet)(nil)
	_ rtccdef.ModeChanger     = (*demoCabinet)(nil)
)

// declarations declare the cabinet's clusters.
func (c *demoCabinet) declarations() []device.Option {
	lo, hi, step, setpoint, auto := cabinetMin, cabinetMax, cabinetStep, cabinetSetpoint, fridgeAuto
	return []device.Option{
		tcdef.Declare{
			Features:          tcdef.FeatureTemperatureNumber | tcdef.FeatureTemperatureStep,
			TemperatureSetter: c,
			Initial:           tcdef.Patch{MinTemperature: &lo, MaxTemperature: &hi, Step: &step, TemperatureSetpoint: &setpoint},
		},
		rtccdef.Declare{ModeChanger: c, Initial: rtccdef.Patch{SupportedModes: fridgeModes(), CurrentMode: &auto}},
	}
}

// SetTemperature implements [tcdef.TemperatureSetter]: the compartment
// takes every setpoint the server has let through.
func (c *demoCabinet) SetTemperature(_ context.Context, target *int16, _ *uint8) error {
	if target != nil {
		slog.Info("cabinet.setpoint", slog.String("device", c.name), slog.Int("centi_celsius", int(*target)))
	}
	return nil
}

// ChangeToMode implements [rtccdef.ModeChanger].
func (c *demoCabinet) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	slog.Info("cabinet.mode", slog.String("device", c.name), slog.Int("mode", int(newMode)))
	return uint8(rtccdef.ModeChangeStatusSuccess), "", nil
}
