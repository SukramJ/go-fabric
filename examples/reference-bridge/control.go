// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/valve"
)

// Out-of-band test control, the two ways CHIP's own example apps take it:
//
//   - --app-pipe <path>: a named pipe carrying one JSON command per line,
//     {"Name": "SimulateLongPress", "EndpointId": 20, ...}. This is the
//     channel the CHIP Python certification cases drive a device through
//     (matter_testing.py write_to_app_pipe; the apps' NamedPipeCommands),
//     and the one matter.js serves for its own test apps
//     (support/chip-testing/src/NamedPipeCommandHandler.ts). The command
//     names CHIP defines are understood as CHIP defines them; the few this
//     daemon adds for its own fleet carry the same shape.
//   - --enable-key <hex>: arms GeneralDiagnostics TestEventTrigger with the
//     given 16-byte enable key (cluster/core EnableTestEventTriggers), the
//     certification mechanism for "make the device raise this condition".
//     The SmokeCoAlarm triggers are CHIP's (src/app/clusters/
//     smoke-co-alarm-server/SmokeCOTestEventTriggerHandler.h).
//
// Both are off unless their flag is given; a real deployment has neither.
// Every applied command is logged as `apppipe.applied name=<Name>` (or
// `apppipe.error`), which is how a driver knows it landed — a FIFO has no
// answer channel.

// pipeCommand is one decoded app-pipe line. Field names follow CHIP's JSON.
type pipeCommand struct {
	Name       string  `json:"Name"`
	EndpointID uint16  `json:"EndpointId"`
	NewState   *bool   `json:"NewState"`
	Occupancy  *uint8  `json:"Occupancy"`
	Device     string  `json:"Device"`
	Operation  string  `json:"Operation"`
	Param      *uint8  `json:"Param"`
	Error      string  `json:"Error"`
	Value      float64 `json:"Value"`
	Event      string  `json:"Event"`
	Jammed     bool    `json:"Jammed"`
	NumPresses *uint8  `json:"MultiPressNumPresses"`
	Unmounted  *uint8  `json:"Unmounted"`
}

// serveAppPipe creates the FIFO (when absent) and applies every command
// written to it until ctx ends. Each writer's close is an EOF; the pipe is
// reopened for the next one.
func serveAppPipe(ctx context.Context, f *fleet, path string, logger *slog.Logger) error {
	if err := makeFIFO(path); err != nil {
		return err
	}
	go func() {
		for ctx.Err() == nil {
			fh, err := os.OpenFile(path, os.O_RDONLY, 0) //nolint:gosec // the operator-chosen pipe path
			if err != nil {
				logger.Warn("apppipe.open", slog.String("err", err.Error()))
				return
			}
			readPipe(f, fh, logger)
			_ = fh.Close()
		}
	}()
	go func() {
		<-ctx.Done()
		// Unblock a pending open by opening the write end once.
		unblockFIFO(path)
	}()
	return nil
}

// readPipe applies the commands of one writer.
func readPipe(f *fleet, r io.Reader, logger *slog.Logger) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cmd pipeCommand
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			logger.Warn("apppipe.error", slog.String("line", line), slog.String("err", err.Error()))
			continue
		}
		if err := f.applyPipeCommand(cmd); err != nil {
			logger.Warn("apppipe.error", slog.String("name", cmd.Name), slog.String("err", err.Error()))
			continue
		}
		logger.Info("apppipe.applied", slog.String("name", cmd.Name))
	}
}

// errUnknownPipeCommand reports a command this daemon does not implement.
var errUnknownPipeCommand = errors.New("not a command this daemon implements")

// applyPipeCommand applies one command.
//
//nolint:gocyclo,cyclop // a flat dispatch table: one case per command
func (f *fleet) applyPipeCommand(cmd pipeCommand) error {
	switch cmd.Name {
	// --- CHIP's own names ---
	case "SimulateLongPress":
		return f.pressButton(true)
	case "SimulateMultiPress":
		// The button has no multi-press (MSM) feature; one press is the
		// whole of what it can narrate.
		if cmd.NumPresses != nil && *cmd.NumPresses != 1 {
			return fmt.Errorf("SimulateMultiPress with %d presses: the button has no MSM feature", *cmd.NumPresses)
		}
		return f.pressButton(false)
	case "LongPress":
		// CHIP's smoke-co-alarm app: a long press of the test button starts
		// the self-test (TC-SMOKECO-2.4 step 37).
		return f.smoke.pressTestButton(context.Background())
	case "SetUnmounted":
		// CHIP's smoke-co-alarm app (TC-SMOKECO-2.7).
		if cmd.Unmounted == nil {
			return errors.New("SetUnmounted needs Unmounted")
		}
		// An unmounted alarm cannot detect: it expresses Inoperative until it
		// is mounted again (TC-SMOKECO-2.7 step 8).
		unmounted := *cmd.Unmounted != 0
		f.smoke.update(func(s *alarm.State) { s.Unmounted, s.Inoperative = unmounted, unmounted })
		return nil
	case "SimulateConfigurationVersionChange":
		// CHIP's name (TC-BRBINFO-3.2): the bridged devices' functionality
		// changed. Every bridged device's version is raised, and the node's.
		if f.configChange == nil {
			return errors.New("SimulateConfigurationVersionChange: no bridge wired")
		}
		return f.configChange()
	case "SetBooleanState":
		if cmd.NewState == nil {
			return errors.New("SetBooleanState needs NewState")
		}
		f.contact.reportFromDevice(*cmd.NewState)
		return nil
	case "SetOccupancy":
		if cmd.Occupancy == nil {
			return errors.New("SetOccupancy needs Occupancy")
		}
		f.occupancy.reportFromDevice(*cmd.Occupancy&1 != 0)
		return nil
	case "OperationalStateChange":
		return f.washerOperation(cmd)
	case "ErrorEvent":
		id, ok := rvcErrors[cmd.Error]
		if !ok {
			return fmt.Errorf("ErrorEvent %q is not an RVC error this daemon knows", cmd.Error)
		}
		return f.vacuum.reportError(id)
	case "Docked":
		return f.vacuum.reportDocked()
	case "Reset":
		return f.vacuum.reset()
	case "ChargerFound", "Charging":
		return f.vacuum.reportState(opstate.StateCharging)
	case "Charged":
		return f.vacuum.reportState(opstate.StateDocked)

	// --- this daemon's own fleet ---
	case "OperationCompletion":
		return f.washer.reportCompletion()
	case "SetSensorValue":
		switch cmd.Device {
		case "humidity":
			f.humidity.reportFromDevice(cmd.Value)
		case "flow":
			f.flow.reportFromDevice(cmd.Value)
		default:
			return fmt.Errorf("SetSensorValue: no sensor %q (humidity, flow)", cmd.Device)
		}
		return nil
	case "SetLocalTemperature":
		f.thermostat.reportTemperature(int16(cmd.Value))
		return nil
	case "SetLockJammed":
		f.lock.reportJam(cmd.Jammed)
		return nil
	case "PumpEvent":
		ev, ok := map[string]uint32{"DryRunning": pump.EventDryRunning, "PumpBlocked": pump.EventPumpBlocked}[cmd.Event]
		if !ok {
			return fmt.Errorf("PumpEvent %q is not DryRunning or PumpBlocked", cmd.Event)
		}
		return f.pump.reportEvent(ev)
	case "SetValveState":
		st := valve.StateClosed
		if cmd.Value != 0 {
			st = valve.StateOpen
		}
		f.valve.reportFromDevice(st)
		return nil
	case "SetSelectorMode":
		return f.selector.reportFromDevice(uint8(cmd.Value))
	case "SetSpeakerLevel":
		f.speaker.reportFromDevice(uint8(cmd.Value))
		return nil
	}
	return fmt.Errorf("%q: %w", cmd.Name, errUnknownPipeCommand)
}

func (f *fleet) pressButton(long bool) error {
	if !f.button.press(long) {
		return fmt.Errorf("button %s has no emitter yet (not mounted)", f.button.name)
	}
	return nil
}

// washerOperation is CHIP's OperationalStateChange for the "Generic"
// operational-state device — the washer here. Operations and the OnFault
// parameter follow the all-clusters app (and matter.js's
// AllClustersTestInstance.ts operationalStateChange).
func (f *fleet) washerOperation(cmd pipeCommand) error {
	if cmd.Device != "" && cmd.Device != "Generic" {
		return fmt.Errorf("OperationalStateChange for device %q: only Generic (the washer)", cmd.Device)
	}
	w := f.washer
	w.build()
	switch cmd.Operation {
	case "Start", "Resume":
		return w.ops.SetOperationalState(opstate.StateRunning)
	case "Pause":
		return w.ops.SetOperationalState(opstate.StatePaused)
	case "Stop":
		return w.ops.SetOperationalState(opstate.StateStopped)
	case "OnFault":
		if cmd.Param == nil {
			return errors.New("OnFault needs Param (an ErrorStateEnum value)")
		}
		if err := w.reportError(opstate.ErrorID(*cmd.Param)); err != nil {
			return err
		}
		if opstate.ErrorID(*cmd.Param) == opstate.ErrorNoError {
			// Clearing the fault resumes the cycle (matter.js
			// AllClustersTestInstance.ts OnFault 0).
			return w.ops.SetOperationalState(opstate.StateRunning)
		}
		return nil
	}
	return fmt.Errorf("OperationalStateChange operation %q: %w", cmd.Operation, errUnknownPipeCommand)
}

// rvcErrors maps the rvc-app's ErrorEvent names to ErrorStateEnum values
// (CHIP examples/rvc-app, RvcAppCommandDelegate).
var rvcErrors = map[string]opstate.ErrorID{
	"NoError":                   opstate.ErrorNoError,
	"UnableToStartOrResume":     opstate.ErrorUnableToStartOrResume,
	"UnableToCompleteOperation": opstate.ErrorUnableToCompleteOperation,
	"CommandInvalidInState":     opstate.ErrorCommandInvalidInState,
	"FailedToFindChargingDock":  opstate.ErrorFailedToFindChargingDock,
	"Stuck":                     opstate.ErrorStuck,
	"DustBinMissing":            opstate.ErrorDustBinMissing,
	"DustBinFull":               opstate.ErrorDustBinFull,
	"WaterTankEmpty":            opstate.ErrorWaterTankEmpty,
	"WaterTankMissing":          opstate.ErrorWaterTankMissing,
	"WaterTankLidOpen":          opstate.ErrorWaterTankLidOpen,
	"MopCleaningPadMissing":     opstate.ErrorMopCleaningPadMissing,
}

// --- TestEventTrigger --------------------------------------------------------

// SmokeCoAlarm test event triggers, CHIP's values
// (SmokeCOTestEventTriggerHandler.h). The low 16 bits of the upper half may
// carry an endpoint (clearEndpointInEventTrigger); this daemon has one alarm.
const (
	smokeTriggerMask              uint64 = 0xFFFF0000_FFFFFFFF
	triggerForceSmokeWarning      uint64 = 0x005c0000_00000090
	triggerForceCOWarning         uint64 = 0x005c0000_00000091
	triggerForceSmokeInterconnect uint64 = 0x005c0000_00000092
	triggerForceMalfunction       uint64 = 0x005c0000_00000093
	triggerForceCOInterconnect    uint64 = 0x005c0000_00000094
	triggerForceLowBatteryWarning uint64 = 0x005c0000_00000095
	triggerForceContaminationHigh uint64 = 0x005c0000_00000096
	triggerForceContaminationLow  uint64 = 0x005c0000_00000097
	triggerForceSensitivityHigh   uint64 = 0x005c0000_00000098
	triggerForceSensitivityLow    uint64 = 0x005c0000_00000099
	triggerForceEndOfLife         uint64 = 0x005c0000_0000009a
	triggerForceSilence           uint64 = 0x005c0000_0000009b
	triggerForceSmokeCritical     uint64 = 0x005c0000_0000009c
	triggerForceCOCritical        uint64 = 0x005c0000_0000009d
	triggerForceLowBatteryCrit    uint64 = 0x005c0000_0000009e
	triggerForceUnmounted         uint64 = 0x005c0000_0000009f
	triggerClearSmoke             uint64 = 0x005c0000_000000a0
	triggerClearCO                uint64 = 0x005c0000_000000a1
	triggerClearSmokeInterconnect uint64 = 0x005c0000_000000a2
	triggerClearMalfunction       uint64 = 0x005c0000_000000a3
	triggerClearCOInterconnect    uint64 = 0x005c0000_000000a4
	triggerClearBatteryLevelLow   uint64 = 0x005c0000_000000a5
	triggerClearContamination     uint64 = 0x005c0000_000000a6
	triggerClearSensitivity       uint64 = 0x005c0000_000000a8
	triggerClearEndOfLife         uint64 = 0x005c0000_000000aa
	triggerClearSilence           uint64 = 0x005c0000_000000ab
	triggerClearUnmounted         uint64 = 0x005c0000_000000ac
)

// testEventTrigger is the daemon's GeneralDiagnostics TestEventTrigger
// handler. It mirrors what CHIP's smoke-co-alarm app does for each trigger
// (HandleSmokeCOTestEventTrigger): set one condition, or clear it.
func (f *fleet) testEventTrigger(_ context.Context, trigger uint64) error {
	set := func(change func(*alarm.State)) error {
		f.smoke.update(change)
		return nil
	}
	switch trigger & smokeTriggerMask {
	case triggerForceSmokeWarning:
		return set(func(s *alarm.State) { s.SmokeState = alarm.AlarmWarning })
	case triggerForceSmokeCritical:
		return set(func(s *alarm.State) { s.SmokeState = alarm.AlarmCritical })
	case triggerClearSmoke:
		return set(func(s *alarm.State) { s.SmokeState = alarm.AlarmNormal })
	case triggerForceCOWarning:
		return set(func(s *alarm.State) { s.COState = alarm.AlarmWarning })
	case triggerForceCOCritical:
		return set(func(s *alarm.State) { s.COState = alarm.AlarmCritical })
	case triggerClearCO:
		return set(func(s *alarm.State) { s.COState = alarm.AlarmNormal })
	case triggerForceSmokeInterconnect:
		return set(func(s *alarm.State) { s.InterconnectSmokeAlarm = alarm.AlarmWarning })
	case triggerClearSmokeInterconnect:
		return set(func(s *alarm.State) { s.InterconnectSmokeAlarm = alarm.AlarmNormal })
	case triggerForceCOInterconnect:
		return set(func(s *alarm.State) { s.InterconnectCOAlarm = alarm.AlarmWarning })
	case triggerClearCOInterconnect:
		return set(func(s *alarm.State) { s.InterconnectCOAlarm = alarm.AlarmNormal })
	case triggerForceMalfunction:
		return set(func(s *alarm.State) { s.HardwareFaultAlert = true })
	case triggerClearMalfunction:
		return set(func(s *alarm.State) { s.HardwareFaultAlert = false })
	case triggerForceLowBatteryWarning:
		return set(func(s *alarm.State) { s.BatteryAlert = alarm.AlarmWarning })
	case triggerForceLowBatteryCrit:
		return set(func(s *alarm.State) { s.BatteryAlert = alarm.AlarmCritical })
	case triggerClearBatteryLevelLow:
		return set(func(s *alarm.State) { s.BatteryAlert = alarm.AlarmNormal })
	case triggerForceContaminationHigh:
		return set(func(s *alarm.State) { s.ContaminationState = alarm.ContaminationCritical })
	case triggerForceContaminationLow:
		return set(func(s *alarm.State) { s.ContaminationState = alarm.ContaminationLow })
	case triggerClearContamination:
		return set(func(s *alarm.State) { s.ContaminationState = alarm.ContaminationNormal })
	case triggerForceSensitivityHigh:
		return set(func(s *alarm.State) { s.SmokeSensitivityLevel = alarm.SensitivityHigh })
	case triggerForceSensitivityLow:
		return set(func(s *alarm.State) { s.SmokeSensitivityLevel = alarm.SensitivityLow })
	case triggerClearSensitivity:
		return set(func(s *alarm.State) { s.SmokeSensitivityLevel = alarm.SensitivityStandard })
	case triggerForceEndOfLife:
		return set(func(s *alarm.State) { s.EndOfServiceAlert = alarm.EndOfServiceExpired })
	case triggerClearEndOfLife:
		return set(func(s *alarm.State) { s.EndOfServiceAlert = alarm.EndOfServiceNormal })
	case triggerForceSilence:
		// A critical alarm cannot be muted (the SmokeCoAlarm "mute" rule
		// CHIP's smoke-co-alarm app applies; TC-SMOKECO-2.5 step 55).
		return set(func(s *alarm.State) {
			if s.SmokeState != alarm.AlarmCritical && s.COState != alarm.AlarmCritical {
				s.DeviceMuted = alarm.Muted
			}
		})
	case triggerClearSilence:
		return set(func(s *alarm.State) { s.DeviceMuted = alarm.NotMuted })
	case triggerForceUnmounted:
		return set(func(s *alarm.State) { s.Unmounted = true })
	case triggerClearUnmounted:
		return set(func(s *alarm.State) { s.Unmounted = false })
	}
	return fmt.Errorf("test event trigger 0x%016X is not one this daemon implements", trigger)
}
