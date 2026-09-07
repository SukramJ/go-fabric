// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// thermoAttrOccupiedHeatingSetpoint / Cooling are 0x0012 / 0x0011 per
// matter.js packages/model/src/standard/elements/thermostat-cluster.element.ts.
const (
	attrLocalTemperature       uint32 = 0x0000
	attrOccupiedCoolingSetpint uint32 = 0x0011
	attrOccupiedHeatingSetpint uint32 = 0x0012
	attrSystemMode             uint32 = 0x001C
)

// setpointPayload encodes a SetpointRaiseLower payload the way the
// firmware sends it: Mode on context tag 0, Amount on tag 1
// (thermostat-cluster.element.ts:322-323).
func setpointPayload(t *testing.T, mode uint8, amount int8) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(0), uint64(mode))
	e.PutInt(tlv.ContextTag(1), int64(amount))
	if err := e.EndContainer(); err != nil {
		t.Fatalf("EndContainer: %v", err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

func readInt16(t *testing.T, srv *thermo.ThermostatServer, attrID uint32) int16 {
	t.Helper()
	raw, ok := srv.MatterRead(attrID)
	if !ok {
		t.Fatalf("MatterRead(0x%04X): ok = false", attrID)
	}
	v, isInt16 := raw.(int16)
	if !isInt16 {
		t.Fatalf("attribute 0x%04X is %T, want int16", attrID, raw)
	}
	return v
}

// TestSetpointRaiseLowerAcceptsEveryPayloadShape drives the command
// through each shape setpointRaiseLowerRequest normalises. Mode and
// Amount are both conformance M (thermostat-cluster.element.ts:322-323),
// and the same (mode, amount) pair must produce the same setpoint
// whichever shape carried it — the encoding is a transport detail, not a
// different command.
//
// Amount is in 0.1 °C steps and setpoints in 0.01 °C, so +15 raises the
// 20.00 °C heating setpoint by 1.5 °C to 2150 (matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts:169
// multiplies the amount by 10).
func TestSetpointRaiseLowerAcceptsEveryPayloadShape(t *testing.T) {
	t.Parallel()
	const amount int8 = 15
	mode := clusterwire.ThermostatSetpointModeHeat

	shapes := map[string]any{
		"typed value":    clusterwire.SetpointRaiseLowerRequest{Mode: mode, Amount: amount},
		"typed pointer":  &clusterwire.SetpointRaiseLowerRequest{Mode: mode, Amount: amount},
		"raw TLV":        setpointPayload(t, mode, amount),
		"bridge tag map": map[uint8]any{0: uint64(mode), 1: int64(amount)},
		"string map":     map[string]any{"mode": mode, "amount": amount},
	}
	for name, fields := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newHeatCool()
			if _, err := srv.MatterInvoke(
				context.Background(), clusterwire.ThermostatCmdSetpointRaiseLower, fields,
			); err != nil {
				t.Fatalf("SetpointRaiseLower: %v", err)
			}
			if got := readInt16(t, srv, attrOccupiedHeatingSetpint); got != 2150 {
				t.Fatalf("OccupiedHeatingSetpoint = %d, want 2150 (20.00 °C raised by 1.5 °C)", got)
			}
		})
	}
}

// TestSetpointRaiseLowerRejectsAPayloadItCannotRead covers the refusal
// paths of the payload normaliser. Each must be an InvalidCommand and
// must leave the setpoint alone: a command the server cannot read must
// not move a heating setpoint to a guessed value.
func TestSetpointRaiseLowerRejectsAPayloadItCannotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]any{
		"nil typed pointer": (*clusterwire.SetpointRaiseLowerRequest)(nil),
		// Mode is conformance M — a payload without it is refused rather
		// than acknowledged as a no-op.
		"tag map without Mode":   map[uint8]any{1: int64(5)},
		"tag map without Amount": map[uint8]any{0: uint64(clusterwire.ThermostatSetpointModeHeat)},
		"tag map Mode not an enum8": map[uint8]any{
			0: "heat", 1: int64(5),
		},
		// Amount is int8 (thermostat-cluster.element.ts:323): a wider
		// value is a malformed field.
		"tag map Amount wider than int8": map[uint8]any{
			0: uint64(clusterwire.ThermostatSetpointModeHeat), 1: int64(200),
		},
		"tag map Amount below int8": map[uint8]any{
			0: uint64(clusterwire.ThermostatSetpointModeHeat), 1: int64(-200),
		},
		"string map without mode":       map[string]any{"amount": int8(5)},
		"string map mode not an enum8":  map[string]any{"mode": "heat"},
		"string map amount not an int8": map[string]any{"mode": uint8(0), "amount": int64(500)},
		"malformed TLV":                 []byte{0x00, 0x01, 0x02},
		"unknown Go type":               3.5,
		// SetpointRaiseLowerModeEnum has exactly Heat 0, Cool 1, Both 2
		// (thermostat-cluster.element.ts:509-513).
		"mode outside the enum": clusterwire.SetpointRaiseLowerRequest{Mode: 9, Amount: 5},
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newHeatCool()
			_, err := srv.MatterInvoke(
				context.Background(), clusterwire.ThermostatCmdSetpointRaiseLower, fields,
			)
			if err == nil {
				t.Fatal("SetpointRaiseLower accepted a payload it cannot read")
			}
			var status im.StatusCodeError
			if !errors.As(err, &status) {
				t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
			}
			if got := status.MatterStatusCode(); got != im.StatusInvalidCommand {
				t.Errorf("status = %v, want InvalidCommand", got)
			}
			if got := readInt16(t, srv, attrOccupiedHeatingSetpint); got != 2000 {
				t.Errorf("OccupiedHeatingSetpoint moved to %d on a rejected payload", got)
			}
		})
	}
}

// TestSetpointRaiseLowerStringMapAmountDefaultsToZero pins the one
// optional-looking field in the string-keyed shape. That map is for
// direct Go callers, and omitting "amount" leaves it at zero — a
// SetpointRaiseLower that names a mode and moves nothing, which must
// still succeed rather than be reported as malformed.
func TestSetpointRaiseLowerStringMapAmountDefaultsToZero(t *testing.T) {
	t.Parallel()
	srv := newHeatCool()
	if _, err := srv.MatterInvoke(
		context.Background(), clusterwire.ThermostatCmdSetpointRaiseLower,
		map[string]any{"mode": clusterwire.ThermostatSetpointModeHeat},
	); err != nil {
		t.Fatalf("SetpointRaiseLower: %v", err)
	}
	if got := readInt16(t, srv, attrOccupiedHeatingSetpint); got != 2000 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want the unchanged 2000", got)
	}
}

// TestSetpointRaiseLowerCoolOnlyBothMovesTheCoolingSetpoint covers the
// Both arm on a server without HEAT. matter.js ThermostatServer.ts:202-219
// clamps the pair together when both features are present; with only COOL
// there is nothing to coordinate and the cooling setpoint moves on its
// own.
func TestSetpointRaiseLowerCoolOnlyBothMovesTheCoolingSetpoint(t *testing.T) {
	t.Parallel()
	srv := newCoolOnly()
	if _, err := srv.MatterInvoke(
		context.Background(), clusterwire.ThermostatCmdSetpointRaiseLower,
		clusterwire.SetpointRaiseLowerRequest{Mode: clusterwire.ThermostatSetpointModeBoth, Amount: -20},
	); err != nil {
		t.Fatalf("SetpointRaiseLower: %v", err)
	}
	if got := readInt16(t, srv, attrOccupiedCoolingSetpint); got != 2400 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2400 (26.00 °C lowered by 2 °C)", got)
	}
	// Without HEAT the heating setpoint is not even readable, so the
	// Both arm cannot have written one a controller would then see.
	if _, ok := srv.MatterRead(attrOccupiedHeatingSetpint); ok {
		t.Error("OccupiedHeatingSetpoint answered on a COOL-only server")
	}
}

// TestSetpointRaiseLowerCoolOnlyRejectsHeatMode pins the feature guard on
// the Cool arm. Mode Cool is conformance COOL and Mode Heat conformance
// HEAT (thermostat-cluster.element.ts:511-512); matter.js
// ThermostatServer.ts:186-196 rejects the mode whose feature is absent
// with InvalidCommand rather than silently doing nothing.
func TestSetpointRaiseLowerCoolOnlyRejectsHeatMode(t *testing.T) {
	t.Parallel()
	srv := newCoolOnly()
	_, err := srv.MatterInvoke(
		context.Background(), clusterwire.ThermostatCmdSetpointRaiseLower,
		clusterwire.SetpointRaiseLowerRequest{Mode: clusterwire.ThermostatSetpointModeHeat, Amount: 5},
	)
	var status im.StatusCodeError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
	}
	if got := status.MatterStatusCode(); got != im.StatusInvalidCommand {
		t.Errorf("status = %v, want InvalidCommand", got)
	}
}

// TestThermostatUnknownCommandIsUnsupported covers the default arm of the
// command dispatch. Only SetpointRaiseLower (id 0x0,
// thermostat-cluster.element.ts:319) is implemented; the weekly-schedule
// commands sit behind the SCH feature this server does not advertise, so
// they must answer UnsupportedCommand.
func TestThermostatUnknownCommandIsUnsupported(t *testing.T) {
	t.Parallel()
	srv := newHeatCool()
	_, err := srv.MatterInvoke(
		context.Background(), clusterwire.ThermostatCmdSetWeeklySchedule, nil,
	)
	var status im.StatusCodeError
	if !errors.As(err, &status) {
		t.Fatalf("err = %v (%T), want an im.StatusCodeError", err, err)
	}
	if got := status.MatterStatusCode(); got != im.StatusUnsupportedCommand {
		t.Errorf("status = %v, want UnsupportedCommand", got)
	}
}

// TestThermostatLocalTemperatureIsNullableAndSettable pins the quality-X
// LocalTemperature attribute (id 0x0). It must be present-but-null before
// a sensor reports, because a nullable attribute answering
// UnsupportedAttribute is a different thing to a controller than one
// answering null.
func TestThermostatLocalTemperatureIsNullableAndSettable(t *testing.T) {
	t.Parallel()
	srv := newHeatCool()

	raw, ok := srv.MatterRead(attrLocalTemperature)
	if !ok {
		t.Fatal("LocalTemperature: ok = false, want present-with-null")
	}
	if raw != nil {
		t.Errorf("LocalTemperature = %v before any sensor report, want null", raw)
	}

	temp := int16(2145)
	srv.SetLocalTemperature(&temp)
	if got := readInt16(t, srv, attrLocalTemperature); got != 2145 {
		t.Errorf("LocalTemperature = %d, want 2145", got)
	}

	// Back to null when the sensor drops out.
	srv.SetLocalTemperature(nil)
	raw, ok = srv.MatterRead(attrLocalTemperature)
	if !ok || raw != nil {
		t.Errorf("LocalTemperature = %v (ok=%v), want present-with-null again", raw, ok)
	}
}

// TestThermostatReadsEveryAdvertisedAttribute walks MatterAttributes on
// each feature profile and requires MatterRead to answer every entry. A
// listed attribute the server cannot read answers UnsupportedAttribute on
// a wildcard read, which chip-tool and Apple Home treat as a conformance
// failure.
func TestThermostatReadsEveryAdvertisedAttribute(t *testing.T) {
	t.Parallel()
	servers := map[string]*thermo.ThermostatServer{
		"heat+cool+auto": newHeatCool(),
		"heat only":      newHeatOnly(),
		"cool only":      newCoolOnly(),
	}
	for name, srv := range servers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, attrID := range srv.MatterAttributes() {
				if _, ok := srv.MatterRead(attrID); !ok {
					t.Errorf("MatterRead(0x%04X): ok = false, but the attribute is advertised", attrID)
				}
			}
			// The two universal globals are answered by the server
			// itself; nothing upstream fills them in.
			if _, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap); !ok {
				t.Error("FeatureMap: ok = false")
			}
			if _, ok := srv.MatterRead(cluster.AttrGlobalClusterRevision); !ok {
				t.Error("ClusterRevision: ok = false")
			}
			// ThermostatRunningMode (0x001E) needs the AUTO feature and
			// this server never serves it.
			if _, ok := srv.MatterRead(0x001E); ok {
				t.Error("ThermostatRunningMode answered, but the server does not implement it")
			}
		})
	}
}

// TestThermostatReportableAttributesAreReadable pins the subscription
// surface. An attribute reported on change that MatterRead cannot answer
// produces a subscription report the controller cannot fill.
func TestThermostatReportableAttributesAreReadable(t *testing.T) {
	t.Parallel()
	srv := newHeatCool()
	reportable := srv.MatterReportable()
	if len(reportable) == 0 {
		t.Fatal("MatterReportable is empty; LocalTemperature and SystemMode change at runtime")
	}
	for _, attrID := range reportable {
		if _, ok := srv.MatterRead(attrID); !ok {
			t.Errorf("MatterRead(0x%04X): ok = false, but the attribute is reportable", attrID)
		}
	}
}

// TestThermostatWriteRejectsWhatItCannotStore covers the refusal arms of
// MatterWrite: a feature-gated setpoint on a server without the feature,
// a non-numeric value, and an attribute that is not writable at all.
// ControlSequenceOfOperation is the interesting one — matter.js declares
// it RW, this server serves it read-only because it follows the device's
// immutable capability, so a write must be refused rather than accepted
// and dropped.
func TestThermostatWriteRejectsWhatItCannotStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("cooling setpoint without COOL", func(t *testing.T) {
		t.Parallel()
		if err := newHeatOnly().MatterWrite(ctx, attrOccupiedCoolingSetpint, int16(2400)); err == nil {
			t.Fatal("OccupiedCoolingSetpoint accepted on a HEAT-only server")
		}
	})
	t.Run("heating setpoint without HEAT", func(t *testing.T) {
		t.Parallel()
		if err := newCoolOnly().MatterWrite(ctx, attrOccupiedHeatingSetpint, int16(2100)); err == nil {
			t.Fatal("OccupiedHeatingSetpoint accepted on a COOL-only server")
		}
	})
	t.Run("non-numeric heating setpoint", func(t *testing.T) {
		t.Parallel()
		if err := newHeatCool().MatterWrite(ctx, attrOccupiedHeatingSetpint, "warm"); err == nil {
			t.Fatal("OccupiedHeatingSetpoint accepted a string")
		}
	})
	t.Run("non-numeric cooling setpoint", func(t *testing.T) {
		t.Parallel()
		if err := newHeatCool().MatterWrite(ctx, attrOccupiedCoolingSetpint, "cold"); err == nil {
			t.Fatal("OccupiedCoolingSetpoint accepted a string")
		}
	})
	t.Run("non-numeric system mode", func(t *testing.T) {
		t.Parallel()
		if err := newHeatCool().MatterWrite(ctx, attrSystemMode, "auto"); err == nil {
			t.Fatal("SystemMode accepted a string")
		}
	})
	t.Run("read-only ControlSequenceOfOperation", func(t *testing.T) {
		t.Parallel()
		srv := newHeatCool()
		if err := srv.MatterWrite(ctx, 0x001B, uint8(2)); err == nil {
			t.Fatal("ControlSequenceOfOperation accepted a write")
		}
		// The value a controller reads is still the derived one:
		// CoolingAndHeating (4) for a HEAT+COOL server
		// (thermostat-cluster.element.ts:517-521 ControlSequenceOfOperationEnum).
		raw, ok := srv.MatterRead(0x001B)
		if !ok || raw.(uint8) != 4 {
			t.Errorf("ControlSequenceOfOperation = %v, want 4 (CoolingAndHeating)", raw)
		}
	})
}

// TestThermostatCoolOnlySystemModeGuard covers the CoolingOnly arm of the
// SystemMode validator. matter.js
// ThermostatServer.ts:#assertSystemModeChanging (615-634) forbids Heat (4)
// and EmergencyHeat (5) under a CoolingOnly control sequence — a cooling
// device told to heat would otherwise report a mode it cannot perform.
func TestThermostatCoolOnlySystemModeGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, mode := range []uint8{4, 5} {
		srv := newCoolOnly()
		err := srv.MatterWrite(ctx, attrSystemMode, mode)
		var status im.StatusCodeError
		if !errors.As(err, &status) {
			t.Fatalf("SystemMode %d: err = %v (%T), want an im.StatusCodeError", mode, err, err)
		}
		if got := status.MatterStatusCode(); got != im.StatusConstraintError {
			t.Errorf("SystemMode %d: status = %v, want ConstraintError", mode, got)
		}
	}
	// Cool (3) is exactly what a CoolingOnly sequence allows.
	srv := newCoolOnly()
	if err := srv.MatterWrite(ctx, attrSystemMode, uint8(3)); err != nil {
		t.Fatalf("SystemMode Cool refused on a COOL-only server: %v", err)
	}
	raw, ok := srv.MatterRead(attrSystemMode)
	if !ok || raw.(uint8) != 3 {
		t.Errorf("SystemMode = %v, want 3 (Cool)", raw)
	}
}

// TestThermostatWithoutHeatOrCoolIsOff pins the default arm of the
// initial-SystemMode choice. A server advertising neither HEAT nor COOL
// can perform nothing, so it must start in Off (0) rather than claim a
// mode it has no feature for.
func TestThermostatWithoutHeatOrCoolIsOff(t *testing.T) {
	t.Parallel()
	srv := thermo.NewThermostatServer(thermo.ThermostatConfig{})
	raw, ok := srv.MatterRead(attrSystemMode)
	if !ok {
		t.Fatal("SystemMode: ok = false")
	}
	if got := raw.(uint8); got != 0 {
		t.Errorf("SystemMode = %d, want 0 (Off) without HEAT or COOL", got)
	}
}
