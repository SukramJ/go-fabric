// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/contract"
)

// The two appliances below run a programme: an OperationalState (or
// RvcOperationalState) server reports where in it they are, and a ModeBase
// server selects which programme. Both servers keep their state themselves,
// as a matter.js behavior does, and fire their own attribute changes; the
// device builds them once and returns the same instances on every
// reassembly, so a rebuilt topology does not reset a running cycle.
//
// What the model does and does not claim: a cycle never ends on its own.
// Completion, errors and the RVC reaching its dock are things the device
// reports, and in this example the debug control hook (control.go) reports
// them on a test's behalf — the same seam a real host drives from its
// southbound event stream.

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
// OperationalState runs it.
type demoWasher struct {
	name string

	once    sync.Once
	ops     *opstate.Server
	modes   *modebase.Server
	version struct{ ops, modes cluster.DataVersionTracker }

	mu      sync.Mutex
	running bool
}

var (
	_ contract.EndpointSource = (*demoWasher)(nil)
	_ opstate.CommandHandler  = (*demoWasher)(nil)
	_ modebase.ModeChanger    = (*demoWasher)(nil)
)

func newDemoWasher(name string) *demoWasher { return &demoWasher{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (w *demoWasher) MatterDeviceType() uint16 { return opstate.DeviceTypeLaundryWasher }

// MatterClusterServers implements [contract.EndpointSource].
func (w *demoWasher) MatterClusterServers() []contract.ClusterServer {
	w.build()
	return []contract.ClusterServer{w.ops, w.modes}
}

// build constructs both servers once. A construction error is a programming
// error in this file — every input is a constant — so it panics rather than
// limping on with a half-built endpoint.
func (w *demoWasher) build() {
	w.once.Do(func() {
		phase := uint8(0)
		ops, err := opstate.NewServer(opstate.Config{
			Handler:  w,
			Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandStart | opstate.CommandStop,
			States: []opstate.StateEntry{
				{ID: opstate.StateStopped},
				{ID: opstate.StateRunning},
				{ID: opstate.StatePaused},
				{ID: opstate.StateError},
			},
			State:               opstate.StateStopped,
			Phases:              []string{"wash", "rinse", "spin"},
			CurrentPhase:        &phase,
			CountdownTime:       true,
			OperationCompletion: true,
			DeviceType:          opstate.DeviceTypeLaundryWasher,
			DataVersion:         &w.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("washer OperationalState: %v", err))
		}
		modes, err := modebase.NewLaundryWasherMode(modebase.Config{
			Changer: w,
			SupportedModes: []modebase.ModeOption{
				{Label: "Normal", Mode: washNormal, Tags: []modebase.ModeTag{{Value: modebase.LaundryTagNormal}}},
				{Label: "Delicate", Mode: washDelicate, Tags: []modebase.ModeTag{{Value: modebase.LaundryTagDelicate}}},
				{Label: "Heavy", Mode: washHeavy, Tags: []modebase.ModeTag{{Value: modebase.LaundryTagHeavy}}},
				{Label: "Locked while running", Mode: washLocked, Tags: []modebase.ModeTag{{Value: modebase.TagQuiet}}},
			},
			CurrentMode: washNormal,
			DataVersion: &w.version.modes,
		})
		if err != nil {
			panic(fmt.Sprintf("washer LaundryWasherMode: %v", err))
		}
		w.ops, w.modes = ops, modes
	})
}

// HandleOperationalCommand implements [opstate.CommandHandler]. The server
// has already refused what is invalid in the current state; what arrives
// here moves the cycle.
func (w *demoWasher) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	next := opstate.StateStopped
	switch cmd {
	case opstate.CommandStart, opstate.CommandResume:
		next = opstate.StateRunning
	case opstate.CommandPause:
		next = opstate.StatePaused
	case opstate.CommandStop:
		next = opstate.StateStopped
	default:
		return opstate.ErrorState{ID: opstate.ErrorCommandInvalidInState}, nil
	}
	w.mu.Lock()
	w.running = next == opstate.StateRunning || next == opstate.StatePaused
	w.mu.Unlock()
	if err := w.ops.SetOperationalState(next); err != nil {
		return opstate.ErrorState{}, err
	}
	if next == opstate.StateRunning {
		remaining := uint32(1800)
		_ = w.ops.SetCountdownTime(&remaining)
	}
	slog.Info("washer.state", slog.String("device", w.name), slog.Int("state", int(next)))
	return opstate.ErrorState{ID: opstate.ErrorNoError}, nil
}

// ChangeToMode implements [modebase.ModeChanger]. The locked programme is
// refused while a cycle runs or is paused — the device decides, and says why.
func (w *demoWasher) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	if newMode == washLocked && running {
		return modebase.StatusInvalidInMode, "not while a cycle runs", nil
	}
	slog.Info("washer.mode", slog.String("device", w.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

// reportError is the device reporting a fault: OperationalState goes to
// Error and the OperationalError event fires (the server does both).
func (w *demoWasher) reportError(id opstate.ErrorID) error {
	w.build()
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
	slog.Info("washer.error", slog.String("device", w.name), slog.Int("error", int(id)))
	return w.ops.SetOperationalError(opstate.ErrorState{ID: id})
}

// reportCompletion is the device finishing its cycle: the OperationCompletion
// event, then Stopped with no countdown.
func (w *demoWasher) reportCompletion() error {
	w.build()
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
	if err := w.ops.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorNoError}); err != nil {
		return err
	}
	if err := w.ops.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorNoError}); err != nil {
		return err
	}
	_ = w.ops.SetCountdownTime(nil)
	slog.Info("washer.complete", slog.String("device", w.name))
	return w.ops.SetOperationalState(opstate.StateStopped)
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

	once    sync.Once
	ops     *opstate.Server
	run     *modebase.Server
	clean   *modebase.Server
	version struct{ ops, run, clean cluster.DataVersionTracker }

	mu      sync.Mutex
	runMode uint8
	paused  bool
}

var (
	_ contract.EndpointSource = (*demoVacuum)(nil)
	_ opstate.CommandHandler  = (*demoVacuum)(nil)
)

func newDemoVacuum(name string) *demoVacuum { return &demoVacuum{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (v *demoVacuum) MatterDeviceType() uint16 { return opstate.DeviceTypeRoboticVacuumCleaner }

// MatterClusterServers implements [contract.EndpointSource].
func (v *demoVacuum) MatterClusterServers() []contract.ClusterServer {
	v.build()
	return []contract.ClusterServer{v.ops, v.run, v.clean}
}

func (v *demoVacuum) build() {
	v.once.Do(func() {
		ops, err := opstate.NewRvcServer(opstate.Config{
			Handler:  v,
			Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandGoHome,
			States: []opstate.StateEntry{
				{ID: opstate.StateStopped},
				{ID: opstate.StateRunning},
				{ID: opstate.StatePaused},
				{ID: opstate.StateError},
				{ID: opstate.StateSeekingCharger},
				{ID: opstate.StateCharging},
				{ID: opstate.StateDocked},
			},
			State:       opstate.StateDocked,
			DeviceType:  opstate.DeviceTypeRoboticVacuumCleaner,
			DataVersion: &v.version.ops,
		})
		if err != nil {
			panic(fmt.Sprintf("vacuum RvcOperationalState: %v", err))
		}
		run, err := modebase.NewRvcRunMode(modebase.Config{
			Changer: rvcRunChanger{v},
			SupportedModes: []modebase.ModeOption{
				{Label: "Idle", Mode: rvcIdle, Tags: []modebase.ModeTag{{Value: modebase.RvcRunTagIdle}}},
				{Label: "Cleaning", Mode: rvcCleaning, Tags: []modebase.ModeTag{{Value: modebase.RvcRunTagCleaning}}},
				{Label: "Mapping", Mode: rvcMapping, Tags: []modebase.ModeTag{{Value: modebase.RvcRunTagMapping}}},
			},
			CurrentMode: rvcIdle,
			DataVersion: &v.version.run,
		})
		if err != nil {
			panic(fmt.Sprintf("vacuum RvcRunMode: %v", err))
		}
		clean, err := modebase.NewRvcCleanMode(modebase.Config{
			Changer: rvcCleanChanger{v},
			SupportedModes: []modebase.ModeOption{
				{Label: "Vacuum", Mode: rvcVacuum, Tags: []modebase.ModeTag{{Value: modebase.RvcCleanTagVacuum}}},
				{Label: "Mop", Mode: rvcMop, Tags: []modebase.ModeTag{{Value: modebase.RvcCleanTagMop}}},
			},
			CurrentMode: rvcVacuum,
			DataVersion: &v.version.clean,
		})
		if err != nil {
			panic(fmt.Sprintf("vacuum RvcCleanMode: %v", err))
		}
		v.ops, v.run, v.clean = ops, run, clean
	})
}

// HandleOperationalCommand implements [opstate.CommandHandler].
func (v *demoVacuum) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	var next opstate.State
	switch cmd {
	case opstate.CommandPause:
		next = opstate.StatePaused
	case opstate.CommandResume:
		next = opstate.StateRunning
	case opstate.CommandGoHome:
		next = opstate.StateSeekingCharger
	default:
		return opstate.ErrorState{ID: opstate.ErrorCommandInvalidInState}, nil
	}
	v.mu.Lock()
	v.paused = next == opstate.StatePaused
	v.mu.Unlock()
	slog.Info("vacuum.state", slog.String("device", v.name), slog.Int("state", int(next)))
	if err := v.ops.SetOperationalState(next); err != nil {
		return opstate.ErrorState{}, err
	}
	return opstate.ErrorState{ID: opstate.ErrorNoError}, nil
}

// rvcRunChanger applies RvcRunMode changes: a cleaning or mapping run
// starts the robot, Idle stops it.
type rvcRunChanger struct{ v *demoVacuum }

func (c rvcRunChanger) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	v := c.v
	next := opstate.StateRunning
	if newMode == rvcIdle {
		next = opstate.StateStopped
	}
	v.mu.Lock()
	v.runMode = newMode
	v.paused = false
	v.mu.Unlock()
	slog.Info("vacuum.run_mode", slog.String("device", v.name), slog.Int("mode", int(newMode)))
	if err := v.ops.SetOperationalState(next); err != nil {
		return modebase.StatusGenericFailure, "", err
	}
	return modebase.StatusSuccess, "", nil
}

// rvcCleanChanger applies RvcCleanMode changes. Switching between vacuum
// and mop mid-run is refused with CleaningInProgress (0x40), the status the
// derived cluster defines for it (rvc-clean-mode.element.ts).
type rvcCleanChanger struct{ v *demoVacuum }

func (c rvcCleanChanger) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	v := c.v
	v.mu.Lock()
	running := v.runMode != rvcIdle
	v.mu.Unlock()
	if running {
		return modebase.StatusCleaningInProgress, "finish the run first", nil
	}
	slog.Info("vacuum.clean_mode", slog.String("device", v.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

// reportDocked is the robot arriving at its dock after GoHome.
func (v *demoVacuum) reportDocked() error {
	v.build()
	v.mu.Lock()
	v.runMode = rvcIdle
	v.mu.Unlock()
	if err := v.run.SetCurrentMode(rvcIdle); err != nil {
		return err
	}
	slog.Info("vacuum.docked", slog.String("device", v.name))
	return v.ops.SetOperationalState(opstate.StateDocked)
}

// reportError is the robot reporting a fault (stuck, dust bin full, …).
func (v *demoVacuum) reportError(id opstate.ErrorID) error {
	v.build()
	slog.Info("vacuum.error", slog.String("device", v.name), slog.Int("error", int(id)))
	return v.ops.SetOperationalError(opstate.ErrorState{ID: id})
}
