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
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/fan"
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
