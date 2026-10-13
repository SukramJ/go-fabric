// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package cluster_test — consolidated negative-write / negative-invoke
// parity suite.
//
// # Purpose
//
// matter.js schema-parity tests (TestParity_*) lock cluster IDs and
// revisions but NOT write-constraint enforcement.  This file closes that
// gap: every row asserts "a write or invoke that matter.js REJECTS is
// also rejected by Loom with the expected IM status code."
//
// # How to add a row
//
//  1. Read the relevant matter.js source (cluster element or behavior).
//  2. Add a negativeWriteCase (or negativeInvokeCase) row to the table
//     inside TestNegativeWriteParity / TestNegativeInvokeParity.
//  3. Set wantStatus to the IM status matter.js raises (StatusConstraintError,
//     StatusInvalidCommand, …).
//  4. Cite the matter.js path:line in the row comment.
//  5. Add a complementary positive-control row (accepted boundary value)
//     to TestPositiveWriteControl / TestPositiveInvokeControl to prove
//     the suite is not rejecting everything.
package cluster_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/spec"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	dishdef "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	rvcopdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rvcrundef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// statusCoder is a local alias for the im.StatusCodeError interface so this
// file needs no direct knowledge of unexported error types.
type statusCoder interface {
	MatterStatusCode() im.StatusCode
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newHeatOnlyServer() *thermo.ThermostatServer {
	return thermo.NewThermostatServer(thermo.ThermostatConfig{
		Features:                thermo.ThermostatFeatureHEAT,
		AbsMinHeatSetpointLimit: 700,
		AbsMaxHeatSetpointLimit: 3000,
		InitialHeatingSetpoint:  2000,
	})
}

func newCoolOnlyServer() *thermo.ThermostatServer {
	return thermo.NewThermostatServer(thermo.ThermostatConfig{
		Features:                thermo.ThermostatFeatureCOOL,
		AbsMinCoolSetpointLimit: 1600,
		AbsMaxCoolSetpointLimit: 3200,
		InitialCoolingSetpoint:  2600,
	})
}

// refusingThermostatSink is a Thermostat host that refuses every change,
// as a matter.js host throwing in occupied*Setpoint$Changing /
// systemMode$Changing does.
type refusingThermostatSink struct{}

func (refusingThermostatSink) SetSystemMode(context.Context, uint8) error {
	return errors.New("device refused")
}

func (refusingThermostatSink) SetSetpoint(context.Context, uint32, int16, []thermo.SetpointChange) error {
	return errors.New("device refused")
}

func newRefusingThermostatServer() *thermo.ThermostatServer {
	cfg := thermo.DefaultThermostatConfig()
	cfg.Sink = refusingThermostatSink{}
	return thermo.NewThermostatServer(cfg)
}

// refusingMover is a WindowCovering host whose device refuses every
// movement.
type refusingMover struct{}

func (refusingMover) GoTo(context.Context, *uint16, *uint16) error {
	return errors.New("device refused")
}

func (refusingMover) Stop(context.Context) error { return errors.New("device refused") }

func newWindowCoveringServer() *cover.WindowCoveringServer {
	return cover.NewWindowCoveringServer(cover.Config{
		Type:           0,
		EndProductType: 0,
		FeatureMap:     0x05, // LF (bit 0) + PA_LF (bit 2)
	})
}

// smokeAlarmSource is a SmokeCoAlarm host that accepts every sensitivity
// write and remembers the level.
type smokeAlarmSource struct{ st alarm.State }

func (s *smokeAlarmSource) SmokeCOState() alarm.State { return s.st }

func (s *smokeAlarmSource) SetSmokeSensitivityLevel(_ context.Context, level alarm.Sensitivity) error {
	s.st.SmokeSensitivityLevel = level
	return nil
}

func newSmokeAlarmServer() *alarm.Server {
	srv, err := alarm.NewServer(alarm.Config{
		Source:   &smokeAlarmSource{st: alarm.State{SmokeSensitivityLevel: alarm.SensitivityStandard}},
		Features: alarm.FeatureSmokeAlarm,
		Optional: alarm.OptionalSmokeSensitivityLevel,
	})
	if err != nil {
		panic(err)
	}
	return srv
}

// fanSource is a FanControl host that accepts every change.
type fanSource struct{ st fan.State }

func (f *fanSource) FanState() fan.State { return f.st }

func (f *fanSource) ApplyFanSettings(_ context.Context, s fan.Settings) error {
	if s.FanMode != nil {
		f.st.FanMode = *s.FanMode
	}
	if p := s.PercentSetting; p != nil && !p.Null {
		f.st.PercentSetting = &p.Value
	}
	if v := s.SpeedSetting; v != nil && !v.Null {
		f.st.SpeedSetting = &v.Value
	}
	return nil
}

func (f *fanSource) SetRockSetting(_ context.Context, r fan.RockBitmap) error {
	f.st.RockSetting = r
	return nil
}

// newFanServer is a fan without Auto (sequence OffLowHigh), with four
// speeds, rocking left-right only, and the Step command.
func newFanServer() *fan.Server {
	srv, err := fan.NewServer(fan.Config{
		Source:      &fanSource{},
		Features:    fan.FeatureMultiSpeed | fan.FeatureRocking | fan.FeatureStep,
		Sequence:    fan.SequenceOffLowHigh,
		SpeedMax:    4,
		RockSupport: fan.RockLeftRight,
	})
	if err != nil {
		panic(err)
	}
	return srv
}

// opstateHost accepts every OperationalState command.
type opstateHost struct{}

func (opstateHost) HandleOperationalCommand(context.Context, opstate.Command) (opstate.ErrorState, error) {
	return opstate.ErrorState{}, nil
}

// newOpStateServer is a washer's OperationalState with Stop alone, or an
// RVC's with Pause / Resume / GoHome, running.
func newOpStateServer(rvc bool) *opstate.Server {
	states := []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StatePaused}, {ID: opstate.StateError}}
	build, cmds := opstate.NewServer, opstate.CommandStop
	if rvc {
		states = append(states, opstate.StateEntry{ID: opstate.StateSeekingCharger})
		build, cmds = opstate.NewRvcServer, opstate.CommandPause|opstate.CommandResume|opstate.CommandGoHome
	}
	srv, err := build(opstate.Config{Handler: opstateHost{}, Commands: cmds, States: states, State: opstate.StateRunning})
	if err != nil {
		panic(err)
	}
	return srv
}

// modeHost accepts every mode change.
type modeHost struct{}

func (modeHost) ChangeToMode(context.Context, uint8) (modebase.Status, string, error) {
	return modebase.StatusSuccess, "", nil
}

// newDishwasherModeServer is a DishwasherMode with Normal (0) and Heavy (1).
func newDishwasherModeServer() *modebase.Server {
	srv, err := modebase.NewDishwasherMode(modebase.Config{Changer: modeHost{}, SupportedModes: []modebase.ModeOption{
		{Label: "Normal", Mode: 0, Tags: []modebase.ModeTag{{Value: modebase.DishwasherTagNormal}}},
		{Label: "Heavy", Mode: 1, Tags: []modebase.ModeTag{{Value: modebase.DishwasherTagHeavy}}},
	}})
	if err != nil {
		panic(err)
	}
	return srv
}

// pumpSource is a PumpConfigurationAndControl host that accepts every
// write.
type pumpSource struct{ st pump.State }

func (p *pumpSource) PumpState() pump.State { return p.st }

func (p *pumpSource) SetOperationMode(_ context.Context, m pump.OperationMode) error {
	p.st.OperationMode = m
	return nil
}

func (p *pumpSource) SetControlMode(_ context.Context, m pump.ControlMode) error {
	p.st.ControlMode = m
	return nil
}

func (p *pumpSource) SetLifetimeRunningHours(_ context.Context, h *uint32) error {
	p.st.LifetimeRunningHours = h
	return nil
}

// newPumpServer is a constant-pressure pump without SPD or LOCAL, serving
// ControlMode and LifetimeRunningHours.
func newPumpServer() *pump.Server {
	srv, err := pump.NewServer(pump.Config{
		Source:   &pumpSource{},
		Features: pump.FeatureConstantPressure,
		Optional: pump.OptionalControlMode | pump.OptionalLifetimeRunningHours,
	})
	if err != nil {
		panic(err)
	}
	return srv
}

// ── negative write cases ─────────────────────────────────────────────────────

type negativeWriteCase struct {
	name  string
	build func() interface {
		MatterWrite(context.Context, uint32, any) error
	}
	attrID     uint32
	value      any
	wantStatus im.StatusCode
}

// TestNegativeWriteParity is the consolidated suite that asserts every
// attribute write that matter.js rejects is rejected by Loom with the
// correct IM status code.
//
// Each row cites the matter.js source path and line number where the
// rejection is defined.  Positive-control rows live in
// TestPositiveWriteControl.
func TestNegativeWriteParity(t *testing.T) {
	t.Parallel()

	cases := []negativeWriteCase{
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:879
			// #assertSetpointWithinLimits — rejects values above MaxHeatSetpointLimit.
			name: "Thermostat/OccupiedHeatingSetpoint above maxHeat → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newHeatOnlyServer()
			},
			attrID:     0x0012, // OccupiedHeatingSetpoint
			value:      int16(3001),
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:879
			// #assertSetpointWithinLimits — rejects values below MinHeatSetpointLimit.
			name: "Thermostat/OccupiedHeatingSetpoint below minHeat → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newHeatOnlyServer()
			},
			attrID:     0x0012, // OccupiedHeatingSetpoint
			value:      int16(699),
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:615-634
			// #assertSystemModeChanging — CoolingOnly sequence forbids SystemMode=Heat(4).
			name: "Thermostat/SystemMode=Heat(4) on CoolingOnly server → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newCoolOnlyServer()
			},
			attrID:     0x001C, // SystemMode
			value:      uint8(4),
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:599-606
			// (4633bfd7): a host refusing in occupiedHeatingSetpoint$Changing
			// fails the write; thermo.Sink is its Go form, a plain error → Failure.
			name: "Thermostat/OccupiedHeatingSetpoint refused by the host Sink → Failure",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newRefusingThermostatServer()
			},
			attrID:     0x0012, // OccupiedHeatingSetpoint
			value:      int16(2100),
			wantStatus: im.StatusFailure,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:619
			// (4633bfd7): a host refusing in systemMode$Changing fails the write.
			name: "Thermostat/SystemMode refused by the host Sink → Failure",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newRefusingThermostatServer()
			},
			attrID:     0x001C, // SystemMode
			value:      uint8(4),
			wantStatus: im.StatusFailure,
		},
		{
			// Mirrors matter.js packages/node/src/standard/elements/window-covering-cluster.element.ts:72
			// liftPercent100thsValue constraint "max 10000".
			name: "WindowCovering/GoToLiftPercentage liftPercent100ths > 10000 → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				// WindowCovering has no direct MatterWrite path for GoToLiftPercentage;
				// the constraint is enforced inside MatterInvoke via extractPercent100ths.
				// This row is intentionally omitted from TestNegativeWriteParity and
				// covered in TestNegativeInvokeParity instead (see note there).
				return newWindowCoveringServer()
			},
			attrID:     0xFFFF, // sentinel — skip: constraint is on invoke, not write
			value:      nil,
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/standard/elements/window-covering-cluster.element.ts:79
			// Mode attribute constraint "max 15".
			name: "WindowCovering/Mode write > 15 → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newWindowCoveringServer()
			},
			attrID:     wire.WindowCoveringAttrMode, // 0x0017
			value:      uint8(16),
			wantStatus: im.StatusConstraintError,
		},
		{
			// matter.js fan-control.element.ts:106 — FanModeEnum Auto has
			// conformance "AUT"; matter.js rejects a member whose
			// conformance fails with ConstraintError
			// (EnumValueConformanceError, protocol/src/action/errors.ts).
			name: "FanControl/FanMode=Auto without AUT → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrFanMode,
			value:      uint64(fan.FanModeAuto),
			wantStatus: im.StatusConstraintError,
		},
		{
			// fan-control.resource.ts:48-51 + :62-63 — Medium is offered
			// only by sequences 0 and 2; any other value is CONSTRAINT_ERROR.
			name: "FanControl/FanMode=Medium on OffLowHigh → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrFanMode,
			value:      uint64(fan.FanModeMedium),
			wantStatus: im.StatusConstraintError,
		},
		{
			// fan-control.element.ts:39 — PercentSetting "max 100".
			name: "FanControl/PercentSetting=101 → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrPercentSetting,
			value:      uint64(101),
			wantStatus: im.StatusConstraintError,
		},
		{
			// fan-control.element.ts:48 — SpeedSetting "max speedMax".
			name: "FanControl/SpeedSetting > SpeedMax → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrSpeedSetting,
			value:      uint64(5),
			wantStatus: im.StatusConstraintError,
		},
		{
			// fan-control.resource.ts:162-164 — a RockSetting bit not in
			// RockSupport is CONSTRAINT_ERROR.
			name: "FanControl/RockSetting outside RockSupport → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrRockSetting,
			value:      uint64(fan.RockUpDown),
			wantStatus: im.StatusConstraintError,
		},
		{
			// fan-control.element.ts:41 — PercentCurrent is access "R V".
			name: "FanControl/PercentCurrent write → UnsupportedWrite",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newFanServer()
			},
			attrID:     fan.AttrPercentCurrent,
			value:      uint64(10),
			wantStatus: im.StatusUnsupportedWrite,
		},
		{
			// matter.js pump-configuration-and-control.element.ts:146 —
			// OperationModeEnum Minimum is conformance "SPD"; the
			// resource (:252-254) answers an unsupported mode with
			// CONSTRAINT_ERROR.
			name: "PumpConfigurationAndControl/OperationMode=Minimum without SPD → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newPumpServer()
			},
			attrID:     pump.AttrOperationMode,
			value:      uint64(pump.OperationMinimum),
			wantStatus: im.StatusConstraintError,
		},
		{
			// pump-configuration-and-control.element.ts:156 — ControlMode
			// ConstantFlow is conformance "FLW" (resource :264-266).
			name: "PumpConfigurationAndControl/ControlMode=ConstantFlow without FLW → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newPumpServer()
			},
			attrID:     pump.AttrControlMode,
			value:      uint64(pump.ControlConstantFlow),
			wantStatus: im.StatusConstraintError,
		},
		{
			// pump-configuration-and-control.element.ts:95 —
			// LifetimeRunningHours is a nullable uint24: 0xFFFFFF is the
			// null sentinel, outside the value range (resource :217).
			name: "PumpConfigurationAndControl/LifetimeRunningHours=0xFFFFFF → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newPumpServer()
			},
			attrID:     pump.AttrLifetimeRunningHours,
			value:      uint64(0xFFFFFF),
			wantStatus: im.StatusConstraintError,
		},
		{
			// matter.js smoke-co-alarm-cluster.element.ts:44 + :82-87 —
			// SmokeSensitivityLevel is a SensitivityEnum (High 0, Standard
			// 1, Low 2); an enum write outside its values fails matter.js
			// TlvEnum validation with ConstraintError.
			name: "SmokeCoAlarm/SmokeSensitivityLevel=3 → ConstraintError",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newSmokeAlarmServer()
			},
			attrID:     alarm.AttrSmokeSensitivityLevel,
			value:      uint64(3),
			wantStatus: im.StatusConstraintError,
		},
		{
			// smoke-co-alarm-cluster.element.ts:28 — ExpressedState is
			// access "R V".
			name: "SmokeCoAlarm/ExpressedState write → UnsupportedWrite",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newSmokeAlarmServer()
			},
			attrID:     alarm.AttrExpressedState,
			value:      uint64(0),
			wantStatus: im.StatusUnsupportedWrite,
		},
		{
			// operational-state.element.ts:40 — OperationalState is
			// access "R V"; matter.js AttributeWriteResponse answers
			// UnsupportedWrite.
			name: "OperationalState/OperationalState write → UnsupportedWrite",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newOpStateServer(false)
			},
			attrID:     opstate.AttrOperationalState,
			value:      uint64(opstate.StateStopped),
			wantStatus: im.StatusUnsupportedWrite,
		},
		{
			// rvc-operational-state.element.ts: OperationalError is
			// inherited "R V".
			name: "RvcOperationalState/OperationalError write → UnsupportedWrite",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newOpStateServer(true)
			},
			attrID:     opstate.AttrOperationalError,
			value:      map[uint8]any{0: uint64(0)},
			wantStatus: im.StatusUnsupportedWrite,
		},
		{
			// mode-base.element.ts:34 — CurrentMode is "R V".
			name: "DishwasherMode/CurrentMode write → UnsupportedWrite",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newDishwasherModeServer()
			},
			attrID:     modebase.AttrCurrentMode,
			value:      uint64(1),
			wantStatus: im.StatusUnsupportedWrite,
		},
		{
			// dishwasher-mode.element.ts — StartUpMode is "X": not an
			// attribute of the server.
			name: "DishwasherMode/StartUpMode write → UnsupportedAttribute",
			build: func() interface {
				MatterWrite(context.Context, uint32, any) error
			} {
				return newDishwasherModeServer()
			},
			attrID:     0x0002,
			value:      uint64(1),
			wantStatus: im.StatusUnsupportedAttribute,
		},
	}

	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Skip rows that delegate to TestNegativeInvokeParity.
			if tc.attrID == 0xFFFF {
				t.Skip("constraint is on invoke path — see TestNegativeInvokeParity")
			}

			srv := tc.build()
			err := srv.MatterWrite(ctx, tc.attrID, tc.value)
			if err == nil {
				t.Fatalf("MatterWrite: expected error with status %s, got nil", tc.wantStatus)
			}
			sc, ok := err.(statusCoder)
			if !ok {
				t.Fatalf("MatterWrite error %v does not implement MatterStatusCode()", err)
			}
			if got := sc.MatterStatusCode(); got != tc.wantStatus {
				t.Errorf("MatterStatusCode() = %s (0x%02X), want %s (0x%02X)", got, uint8(got), tc.wantStatus, uint8(tc.wantStatus))
			}
		})
	}
}

// ── negative invoke cases ─────────────────────────────────────────────────────

type negativeInvokeCase struct {
	name  string
	build func() interface {
		MatterInvoke(context.Context, uint32, any) (any, error)
	}
	cmdID      uint32
	fields     any
	wantStatus im.StatusCode
}

// TestNegativeInvokeParity asserts that Matter commands matter.js rejects
// are also rejected by Loom with the correct IM status code.
func TestNegativeInvokeParity(t *testing.T) {
	t.Parallel()

	cases := []negativeInvokeCase{
		{
			// matter.js fan-control.element.ts:71 + :88-92 — Direction is a
			// StepDirectionEnum (Increase 0, Decrease 1); an undefined
			// member is UnknownEnumValueError, ConstraintError.
			name: "FanControl/Step Direction=2 → ConstraintError",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newFanServer()
			},
			cmdID:      fan.CmdStep,
			fields:     wire.FanStepRequest{Direction: 2},
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:158-166
			// setpointRaiseLower — mode=Heat without HEAT feature → InvalidCommand.
			// Heat is 0x0 per SetpointRaiseLowerModeEnum
			// (thermostat-cluster.element.ts:511); the payload is the bridge's
			// tag-map shape (field 0 Mode, field 1 Amount, element :322-323).
			name: "Thermostat/SetpointRaiseLower mode=Heat without HEAT feature → InvalidCommand",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newCoolOnlyServer()
			},
			cmdID:      0x00, // SetpointRaiseLower
			fields:     map[uint8]any{0: uint64(wire.ThermostatSetpointModeHeat), 1: int64(5)},
			wantStatus: im.StatusInvalidCommand,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:187-219
			// (4633bfd7): setpointRaiseLower assigns the setpoints, which pass
			// occupied*Setpoint$Changing (:599-606) where a host refuses.
			name: "Thermostat/SetpointRaiseLower refused by the host Sink → Failure",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newRefusingThermostatServer()
			},
			cmdID:      0x00, // SetpointRaiseLower
			fields:     map[uint8]any{0: uint64(wire.ThermostatSetpointModeBoth), 1: int64(5)},
			wantStatus: im.StatusFailure,
		},
		{
			// Mirrors matter.js packages/node/src/standard/elements/window-covering-cluster.element.ts:72
			// GoToLiftPercentage liftPercent100thsValue constraint "max 10000".
			name: "WindowCovering/GoToLiftPercentage liftPercent100ths > 10000 → ConstraintError",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newWindowCoveringServer()
			},
			cmdID: wire.WindowCoveringCmdGoToLiftPercentage, // 0x05
			fields: map[string]any{
				"percent": uint16(10001),
			},
			wantStatus: im.StatusConstraintError,
		},
		{
			// Mirrors matter.js packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:338
			// (4633bfd7): a host's handleMovement that throws fails the command;
			// cover.Mover is its Go form, an error → Failure.
			name: "WindowCovering/UpOrOpen refused by the host Mover → Failure",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0x05, Mover: refusingMover{}})
			},
			cmdID:      wire.WindowCoveringCmdUpOrOpen,
			wantStatus: im.StatusFailure,
		},
		{
			// rvc-operational-state.element.ts:23 — Start is "X" in the
			// derivation: not in AcceptedCommandList, UnsupportedCommand.
			name: "RvcOperationalState/Start → UnsupportedCommand",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newOpStateServer(true)
			},
			cmdID:      opstate.CmdStart,
			wantStatus: im.StatusUnsupportedCommand,
		},
		{
			// operational-state.element.ts:65-68 — Start is "O"; a server
			// that does not support it does not accept it.
			name: "OperationalState/Start on a Stop-only server → UnsupportedCommand",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newOpStateServer(false)
			},
			cmdID:      opstate.CmdStart,
			wantStatus: im.StatusUnsupportedCommand,
		},
		{
			// mode-base.element.ts:49 — NewMode is mandatory; a request the
			// schema does not decode is InvalidCommand.
			name: "DishwasherMode/ChangeToMode without NewMode → InvalidCommand",
			build: func() interface {
				MatterInvoke(context.Context, uint32, any) (any, error)
			} {
				return newDishwasherModeServer()
			},
			cmdID:      modebase.CmdChangeToMode,
			fields:     map[uint8]any{},
			wantStatus: im.StatusInvalidCommand,
		},
	}

	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := tc.build()
			_, err := srv.MatterInvoke(ctx, tc.cmdID, tc.fields)
			if err == nil {
				t.Fatalf("MatterInvoke: expected error with status %s, got nil", tc.wantStatus)
			}
			sc, ok := err.(statusCoder)
			if !ok {
				t.Fatalf("MatterInvoke error %v does not implement MatterStatusCode()", err)
			}
			if got := sc.MatterStatusCode(); got != tc.wantStatus {
				t.Errorf("MatterStatusCode() = %s (0x%02X), want %s (0x%02X)", got, uint8(got), tc.wantStatus, uint8(tc.wantStatus))
			}
		})
	}
}

// ── positive control cases ────────────────────────────────────────────────────

// TestPositiveWriteControl verifies that boundary values matter.js ACCEPTS are
// not rejected by Loom.  These rows guard against over-rejection: if any of
// these fail, the negative-write suite is broken and rejecting valid writes.
func TestPositiveWriteControl(t *testing.T) {
	t.Parallel()

	t.Run("Thermostat/OccupiedHeatingSetpoint == maxHeat accepted", func(t *testing.T) {
		t.Parallel()
		// maxHeat = 3000; write exactly 3000 → must succeed.
		srv := newHeatOnlyServer()
		if err := srv.MatterWrite(context.Background(), 0x0012, int16(3000)); err != nil {
			t.Fatalf("write at maxHeat boundary: %v", err)
		}
		v, ok := srv.MatterRead(0x0012)
		if !ok {
			t.Fatal("OccupiedHeatingSetpoint read after boundary write: ok=false")
		}
		if v.(int16) != 3000 {
			t.Errorf("OccupiedHeatingSetpoint = %d, want 3000", v.(int16))
		}
	})

	t.Run("WindowCovering/Mode write == 15 accepted", func(t *testing.T) {
		t.Parallel()
		// Mode constraint max 15; write exactly 15 → must succeed.
		srv := newWindowCoveringServer()
		if err := srv.MatterWrite(context.Background(), wire.WindowCoveringAttrMode, uint8(15)); err != nil {
			t.Fatalf("write Mode=15 (boundary): %v", err)
		}
		v, ok := srv.MatterRead(wire.WindowCoveringAttrMode)
		if !ok {
			t.Fatal("Mode read after boundary write: ok=false")
		}
		if v.(uint8) != 15 {
			t.Errorf("Mode = %d, want 15", v.(uint8))
		}
	})
}

// TestPositiveWriteControlApplicationClusters holds the accepted
// boundaries of the application clusters' writable attributes.
func TestPositiveWriteControlApplicationClusters(t *testing.T) {
	t.Parallel()

	t.Run("FanControl/PercentSetting == 100 and SpeedSetting == SpeedMax accepted", func(t *testing.T) {
		t.Parallel()
		srv := newFanServer()
		if err := srv.MatterWrite(context.Background(), fan.AttrPercentSetting, uint64(100)); err != nil {
			t.Fatalf("PercentSetting 100: %v", err)
		}
		if err := srv.MatterWrite(context.Background(), fan.AttrSpeedSetting, uint64(4)); err != nil {
			t.Fatalf("SpeedSetting 4 (SpeedMax): %v", err)
		}
		if v, _ := srv.MatterRead(fan.AttrPercentSetting); v != uint8(100) {
			t.Errorf("PercentSetting after SpeedSetting=SpeedMax = %v, want 100 (speed rule)", v)
		}
	})

	t.Run("PumpConfigurationAndControl/ControlMode == ConstantPressure and LifetimeRunningHours == 0xFFFFFE accepted", func(t *testing.T) {
		t.Parallel()
		srv := newPumpServer()
		if err := srv.MatterWrite(context.Background(), pump.AttrControlMode, uint64(pump.ControlConstantPressure)); err != nil {
			t.Fatalf("ControlMode ConstantPressure: %v", err)
		}
		if err := srv.MatterWrite(context.Background(), pump.AttrLifetimeRunningHours, uint64(0xFFFFFE)); err != nil {
			t.Fatalf("LifetimeRunningHours 0xFFFFFE: %v", err)
		}
		if v, _ := srv.MatterRead(pump.AttrLifetimeRunningHours); v != uint32(0xFFFFFE) {
			t.Errorf("LifetimeRunningHours = %v", v)
		}
	})

	t.Run("SmokeCoAlarm/SmokeSensitivityLevel == Low(2) accepted", func(t *testing.T) {
		t.Parallel()
		srv := newSmokeAlarmServer()
		if err := srv.MatterWrite(context.Background(), alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityLow)); err != nil {
			t.Fatalf("write Low: %v", err)
		}
		if v, _ := srv.MatterRead(alarm.AttrSmokeSensitivityLevel); v != uint8(alarm.SensitivityLow) {
			t.Errorf("SmokeSensitivityLevel = %v, want 2", v)
		}
	})
}

// TestPositiveInvokeControl verifies that boundary command arguments
// matter.js accepts are not rejected by Loom.
func TestPositiveInvokeControl(t *testing.T) {
	t.Parallel()

	t.Run("DishwasherMode/ChangeToMode Heavy answered Success", func(t *testing.T) {
		t.Parallel()
		srv := newDishwasherModeServer()
		resp, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, wire.ChangeToModeRequest{NewMode: 1})
		if err != nil || resp.(wire.ChangeToModeResponse).Status != 0 {
			t.Fatalf("ChangeToMode 1: %+v, %v", resp, err)
		}
		if v, _ := srv.MatterRead(modebase.AttrCurrentMode); v != uint8(1) {
			t.Errorf("CurrentMode = %v", v)
		}
	})

	t.Run("RvcOperationalState/GoHome while Running answered NoError", func(t *testing.T) {
		t.Parallel()
		resp, err := newOpStateServer(true).MatterInvoke(context.Background(), opstate.CmdGoHome, nil)
		if err != nil {
			t.Fatalf("GoHome: %v", err)
		}
		if r := resp.(wire.OperationalCommandResponse); r.CommandResponseState.ErrorStateID != 0 {
			t.Errorf("GoHome = %+v, want NoError", r)
		}
	})

	t.Run("WindowCovering/GoToLiftPercentage == 10000 accepted", func(t *testing.T) {
		t.Parallel()
		// liftPercent100ths == 10000 is the maximum valid value (fully closed).
		srv := newWindowCoveringServer()
		_, err := srv.MatterInvoke(
			context.Background(),
			wire.WindowCoveringCmdGoToLiftPercentage,
			map[string]any{"percent": uint16(10000)},
		)
		if err != nil {
			t.Fatalf("GoToLiftPercentage(10000) boundary: %v", err)
		}
		v, ok := srv.MatterRead(wire.WindowCoveringAttrCurrentPositionLiftPercent100ths)
		if !ok {
			t.Fatal("CurrentPositionLiftPercent100ths read after invoke: ok=false")
		}
		if v.(uint16) != 10000 {
			t.Errorf("CurrentPositionLiftPercent100ths = %d, want 10000", v.(uint16))
		}
	})
}

// TestEnumManufacturerRangeParity holds the manufacturer ranges the
// snapshot carries as enum range members (matter.js 9892fa39:
// mode-base.element.ts ModeChangeStatus MfgCodes 128..191 and ModeTag
// MfgTags 32768..49151, operational-state.element.ts OperationalStateEnum
// ManufacturerStates and ErrorStateEnum ManufacturerError 128..191,
// closure-control.element.ts ClosureErrorEnum ManufacturerError 128..191)
// to matter.js's enum validator: a value inside a range is valid, the first
// value past it is ConstraintError
// (packages/node/src/behavior/state/validation/ValueValidator.ts:172-178).
//
// No enum of these backs a writable attribute (every one is read-only or
// sits inside a read-only struct), so the rows go against
// spec.EnumSupported for each shape and, where an attribute is typed by the
// enum directly, against spec.Instance.CheckValue — the value checks
// ValidateWrite makes after access.
func TestEnumManufacturerRangeParity(t *testing.T) {
	t.Parallel()
	ctx := spec.Context(opdef.Definition, 0)
	for _, c := range []struct {
		name   string
		def    *spec.Enum
		lo, hi uint64
	}{
		{"RvcRunMode.ModeChangeStatus.MfgCodes", rvcrundef.ModeChangeStatusDef, 128, 191},
		{"RvcRunMode.ModeTag.MfgTags", rvcrundef.ModeTagDef, 32768, 49151},
		{"DishwasherMode.ModeTag.MfgTags", dishdef.ModeTagDef, 32768, 49151},
		{"OperationalState.OperationalStateEnum.ManufacturerStates", opdef.OperationalStateEnumDef, 128, 191},
		{"OperationalState.ErrorStateEnum.ManufacturerError", opdef.ErrorStateEnumDef, 128, 191},
		{"RvcOperationalState.ErrorStateEnum.ManufacturerError", rvcopdef.ErrorStateEnumDef, 128, 191},
		{"ClosureControl.ClosureErrorEnum.ManufacturerError", closuredef.ClosureErrorEnumDef, 128, 191},
	} {
		for _, v := range []uint64{c.lo, c.hi} {
			if !spec.EnumSupported(c.def, v, ctx) {
				t.Errorf("%s: %d refused, matter.js accepts it", c.name, v)
			}
		}
		if spec.EnumSupported(c.def, c.hi+1, ctx) {
			t.Errorf("%s: %d accepted, matter.js answers ConstraintError", c.name, c.hi+1)
		}
		// The value just below each range is not a named value either.
		if spec.EnumSupported(c.def, c.lo-1, ctx) {
			t.Errorf("%s: %d accepted, matter.js answers ConstraintError", c.name, c.lo-1)
		}
	}

	inst, err := spec.New(opdef.Definition, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.CheckValue(opdef.AttrOperationalState, uint8(191), nil); err != nil {
		t.Errorf("OperationalState 191 (ManufacturerStates): %v", err)
	}
	_, err = inst.CheckValue(opdef.AttrOperationalState, uint8(192), nil)
	var sc statusCoder
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("OperationalState 192: err = %v, want CONSTRAINT_ERROR", err)
	}
	// A read-only attribute stays UNSUPPORTED_WRITE whatever the value.
	_, err = inst.ValidateWrite(opdef.AttrOperationalState, uint8(191), nil)
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusUnsupportedWrite {
		t.Errorf("write OperationalState 191: err = %v, want UNSUPPORTED_WRITE", err)
	}
}
