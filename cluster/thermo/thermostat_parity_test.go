// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/thermo"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestParity_Thermostat_SystemMode_ControlSequenceConsistency verifies
// that the SystemMode initial value is compatible with the server's
// feature-set: a HEAT-only server must start in Heat mode (4), a
// COOL-only server in Cool mode (3), and a HEAT+COOL+AUTO server in
// Auto mode (1). This mirrors the conformance rules in matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts for
// SystemMode defaults and the corresponding ControlSequenceOfOperation
// values expected per Matter §4.3.7.24.
func TestParity_Thermostat_SystemMode_ControlSequenceConsistency(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		srv         *thermo.ThermostatServer
		wantMode    uint8
		wantFeature uint32
	}{
		{
			name:        "HEAT-only → SystemMode=4",
			srv:         newHeatOnly(),
			wantMode:    4,
			wantFeature: thermo.ThermostatFeatureHEAT,
		},
		{
			name:        "COOL-only → SystemMode=3",
			srv:         newCoolOnly(),
			wantMode:    3,
			wantFeature: thermo.ThermostatFeatureCOOL,
		},
		{
			name:     "HEAT+COOL+AUTO → SystemMode=1",
			srv:      newHeatCool(),
			wantMode: 1,
			wantFeature: thermo.ThermostatFeatureHEAT |
				thermo.ThermostatFeatureCOOL |
				thermo.ThermostatFeatureAUTO,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, ok := tc.srv.MatterRead(0x001C) // SystemMode
			if !ok {
				t.Fatal("SystemMode: ok=false")
			}
			if got := v.(uint8); got != tc.wantMode {
				t.Errorf("SystemMode = %d, want %d", got, tc.wantMode)
			}
			fv, ok := tc.srv.MatterRead(0xFFFC) // FeatureMap
			if !ok {
				t.Fatal("FeatureMap: ok=false")
			}
			if got := fv.(uint32); got != tc.wantFeature {
				t.Errorf("FeatureMap = 0x%08X, want 0x%08X", got, tc.wantFeature)
			}
		})
	}
}

type statusCoder interface{ MatterStatusCode() im.StatusCode }

// TestParityMatterJS_Thermostat_SetpointConstraintError verifies that
// writing OccupiedHeatingSetpoint / OccupiedCoolingSetpoint outside
// [min, max] returns ConstraintError (0x87). Mirrors matter.js
// ThermostatServer.ts:#assertSetpointWithinLimits (lines 879-892).
func TestParityMatterJS_Thermostat_SetpointConstraintError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cases := []struct {
		name   string
		srv    *thermo.ThermostatServer
		attrID uint32
		value  int16
	}{
		{
			name:   "heat below min (700)",
			srv:    newHeatOnly(),
			attrID: 0x0012,
			value:  int16(600),
		},
		{
			name:   "heat above max (3000)",
			srv:    newHeatOnly(),
			attrID: 0x0012,
			value:  int16(3100),
		},
		{
			name:   "cool below min (1600)",
			srv:    newCoolOnly(),
			attrID: 0x0011,
			value:  int16(1500),
		},
		{
			name:   "cool above max (3200)",
			srv:    newCoolOnly(),
			attrID: 0x0011,
			value:  int16(3300),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.srv.MatterWrite(ctx, tc.attrID, tc.value)
			if err == nil {
				t.Fatal("expected ConstraintError, got nil")
			}
			sc, ok := err.(statusCoder)
			if !ok {
				t.Fatalf("error %v does not implement MatterStatusCode()", err)
			}
			if sc.MatterStatusCode() != im.StatusConstraintError {
				t.Errorf("MatterStatusCode()=0x%02X, want StatusConstraintError (0x87)", sc.MatterStatusCode())
			}
		})
	}
}

// TestParityMatterJS_Thermostat_SetpointWithinLimitsAccepted verifies that
// valid setpoints are accepted. Mirrors matter.js ThermostatServer.ts:732,879.
func TestParityMatterJS_Thermostat_SetpointWithinLimitsAccepted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := newHeatOnly()
	if err := srv.MatterWrite(ctx, 0x0012, int16(2500)); err != nil {
		t.Fatalf("write within limits: %v", err)
	}
	v, _ := srv.MatterRead(0x0012)
	if v.(int16) != 2500 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want 2500", v.(int16))
	}
}

// TestParityMatterJS_Thermostat_SystemModeConstraintError verifies that
// forbidden SystemMode values return ConstraintError (0x87). Mirrors
// matter.js ThermostatServer.ts:#assertSystemModeChanging (lines 615-634).
func TestParityMatterJS_Thermostat_SystemModeConstraintError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cases := []struct {
		name string
		srv  *thermo.ThermostatServer
		mode uint8 // forbidden mode
	}{
		{"cooling-only forbids Heat(4)", newCoolOnly(), 4},
		{"cooling-only forbids EmergencyHeat(5)", newCoolOnly(), 5},
		{"heating-only forbids Cool(3)", newHeatOnly(), 3},
		{"heating-only forbids Precooling(7)", newHeatOnly(), 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.srv.MatterWrite(ctx, 0x001C, tc.mode)
			if err == nil {
				t.Fatal("expected ConstraintError, got nil")
			}
			sc, ok := err.(statusCoder)
			if !ok {
				t.Fatalf("error %v does not implement MatterStatusCode()", err)
			}
			if sc.MatterStatusCode() != im.StatusConstraintError {
				t.Errorf("MatterStatusCode()=0x%02X, want StatusConstraintError (0x87)", sc.MatterStatusCode())
			}
		})
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerInvalidCommand verifies
// that SetpointRaiseLower returns InvalidCommand for Heat mode without the
// HEAT feature and Cool mode without the COOL feature. Mirrors matter.js
// ThermostatServer.ts:setpointRaiseLower (lines 158-165).
func TestParityMatterJS_Thermostat_SetpointRaiseLowerInvalidCommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Mode values come from SetpointRaiseLowerModeEnum in matter.js
	// packages/model/src/standard/elements/thermostat-cluster.element.ts:510-514:
	// Heat 0x0, Cool 0x1, Both 0x2 — the constants in cluster/wire pin them.
	cases := []struct {
		name string
		srv  *thermo.ThermostatServer
		mode uint8
	}{
		{"cool-only forbids mode=Heat(0)", newCoolOnly(), clusterwire.ThermostatSetpointModeHeat},
		{"heat-only forbids mode=Cool(1)", newHeatOnly(), clusterwire.ThermostatSetpointModeCool},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The bridge's generic salvage path shape: field 0 Mode, field
			// 1 Amount (thermostat-cluster.element.ts:322-323), unsigned as
			// uint64 and signed as int64 (bridge/fields_reader.go
			// decodeGenericTagMap).
			fields := map[uint8]any{0: uint64(tc.mode), 1: int64(10)}
			_, err := tc.srv.MatterInvoke(ctx, 0x00, fields)
			if err == nil {
				t.Fatal("expected InvalidCommand, got nil")
			}
			sc, ok := err.(statusCoder)
			if !ok {
				t.Fatalf("error %v does not implement MatterStatusCode()", err)
			}
			if sc.MatterStatusCode() != im.StatusInvalidCommand {
				t.Errorf("MatterStatusCode()=0x%02X, want StatusInvalidCommand (0x85)", sc.MatterStatusCode())
			}
		})
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerAppliesDelta verifies that
// SetpointRaiseLower adjusts the setpoint by amount*10 units. Mirrors
// matter.js ThermostatServer.ts:setpointRaiseLower line 169 (amount *= 10).
func TestParityMatterJS_Thermostat_SetpointRaiseLowerAppliesDelta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := newHeatOnly() // initial occupHeat = 2000
	// mode=Heat (0x0, thermostat-cluster.element.ts:511), amount=5 →
	// delta = 5*10 = 50 → new = 2050. Bridge tag-map shape.
	fields := map[uint8]any{0: uint64(clusterwire.ThermostatSetpointModeHeat), 1: int64(5)}
	if _, err := srv.MatterInvoke(ctx, 0x00, fields); err != nil {
		t.Fatalf("SetpointRaiseLower: %v", err)
	}
	v, _ := srv.MatterRead(0x0012)
	if v.(int16) != 2050 {
		t.Errorf("OccupiedHeatingSetpoint after raise = %d, want 2050", v.(int16))
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerClampsToLimits verifies
// that SetpointRaiseLower clamps to limits rather than exceeding them.
// Mirrors matter.js ThermostatServer.ts:#clampSetpointToLimits (lines 864-874).
func TestParityMatterJS_Thermostat_SetpointRaiseLowerClampsToLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := newHeatOnly() // maxHeat = 3000, initial = 2000
	// amount=100 → delta=1000 → 2000+1000=3000; with mode=Heat (0x0)
	// this is the boundary, so drive further with a second call that
	// must clamp: 3000+1000=4000 > 3000 → 3000.
	fields := map[uint8]any{0: uint64(clusterwire.ThermostatSetpointModeHeat), 1: int64(100)}
	if _, err := srv.MatterInvoke(ctx, 0x00, fields); err != nil {
		t.Fatalf("SetpointRaiseLower clamp (first): %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, 0x00, fields); err != nil {
		t.Fatalf("SetpointRaiseLower clamp: %v", err)
	}
	v, _ := srv.MatterRead(0x0012)
	if v.(int16) != 3000 {
		t.Errorf("OccupiedHeatingSetpoint clamped = %d, want 3000", v.(int16))
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerModeEnum pins the mode
// dispatch against SetpointRaiseLowerModeEnum
// (thermostat-cluster.element.ts:510-514: Heat 0x0, Cool 0x1, Both 0x2)
// and matter.js ThermostatServer.ts:201-233 (Both moves both setpoints,
// Heat only the heating one, Cool only the cooling one). A handler that
// reads 0 as Both and 1 as Heat answers Success while moving the wrong
// setpoint, which no status code reveals.
func TestParityMatterJS_Thermostat_SetpointRaiseLowerModeEnum(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name     string
		mode     uint8
		wantHeat int16
		wantCool int16
	}{
		{"Heat moves only the heating setpoint", clusterwire.ThermostatSetpointModeHeat, 2030, 2600},
		{"Cool moves only the cooling setpoint", clusterwire.ThermostatSetpointModeCool, 2000, 2630},
		{"Both moves both setpoints", clusterwire.ThermostatSetpointModeBoth, 2030, 2630},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newHeatCool() // heat 2000, cool 2600
			fields := map[uint8]any{0: uint64(tc.mode), 1: int64(3)}
			if _, err := srv.MatterInvoke(ctx, 0x00, fields); err != nil {
				t.Fatalf("SetpointRaiseLower(mode=%d): %v", tc.mode, err)
			}
			heat, _ := srv.MatterRead(0x0012)
			cool, _ := srv.MatterRead(0x0011)
			if heat.(int16) != tc.wantHeat || cool.(int16) != tc.wantCool {
				t.Errorf("mode=%d: heat=%d cool=%d, want heat=%d cool=%d",
					tc.mode, heat.(int16), cool.(int16), tc.wantHeat, tc.wantCool)
			}
		})
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerAcceptsEveryPayloadShape
// pins that the typed request (a host decoding the command itself) and
// the bridge tag map land on the same setpoint.
func TestParityMatterJS_Thermostat_SetpointRaiseLowerAcceptsEveryPayloadShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	shapes := []struct {
		name   string
		fields any
	}{
		{"typed request", clusterwire.SetpointRaiseLowerRequest{Mode: clusterwire.ThermostatSetpointModeHeat, Amount: 5}},
		{"typed request pointer", &clusterwire.SetpointRaiseLowerRequest{Mode: clusterwire.ThermostatSetpointModeHeat, Amount: 5}},
		{"bridge tag map", map[uint8]any{0: uint64(clusterwire.ThermostatSetpointModeHeat), 1: int64(5)}},
	}
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newHeatOnly()
			if _, err := srv.MatterInvoke(ctx, 0x00, tc.fields); err != nil {
				t.Fatalf("SetpointRaiseLower(%T): %v", tc.fields, err)
			}
			v, _ := srv.MatterRead(0x0012)
			if v.(int16) != 2050 {
				t.Errorf("OccupiedHeatingSetpoint = %d, want 2050", v.(int16))
			}
		})
	}
}

// TestParityMatterJS_Thermostat_SetpointRaiseLowerWithoutModeIsRefused
// pins that a request without the mandatory Mode field
// (thermostat-cluster.element.ts:322 conformance "M") is an error, not a
// silent Success that leaves every setpoint where it was.
func TestParityMatterJS_Thermostat_SetpointRaiseLowerWithoutModeIsRefused(t *testing.T) {
	t.Parallel()
	srv := newHeatOnly()
	var empty map[uint8]any
	if _, err := srv.MatterInvoke(context.Background(), 0x00, empty); err == nil {
		t.Fatal("SetpointRaiseLower(empty tag map) = Success, want an error for the missing mandatory Mode")
	}
}
