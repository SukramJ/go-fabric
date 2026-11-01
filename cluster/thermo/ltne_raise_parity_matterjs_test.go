// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
)

// TestParityMatterJS_LocalTemperatureNotExposed: with LTNE the
// LocalTemperature attribute stays null whatever the host reports and no
// LocalTemperatureChange is emitted — matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts:456-464,
// :516-519 at 862ad75f (W3-clua1-03).
func TestParityMatterJS_LocalTemperatureNotExposed(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featHeatTEVT|thermo.ThermostatFeatureLTNE)
	v := int16(2150)
	srv.SetLocalTemperature(&v)
	if got, ok := srv.MatterRead(tdef.AttrLocalTemperature); !ok || got != nil {
		t.Fatalf("LocalTemperature %v, %v under LTNE; want null", got, ok)
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("events %+v under LTNE, want none", got)
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrLocalTemperature: int16(2200)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.MatterRead(tdef.AttrLocalTemperature); got != nil {
		t.Fatalf("LocalTemperature %v after the host's set under LTNE; want null", got)
	}
}

// TestParityMatterJS_SetpointRaiseLowerDoesNotWrap: the raise is computed
// in a wide integer and cropped to the limit, as matter.js computes it in
// numbers (ThermostatServer.ts:200, :222-224, cropValueRange :875-885 at
// 862ad75f); an int16 sum used to wrap and land on the minimum
// (W3-clua1-04).
func TestParityMatterJS_SetpointRaiseLowerDoesNotWrap(t *testing.T) {
	t.Parallel()
	srv, _ := tevtServer(t, featHeatTEVT, func(c *thermo.ThermostatConfig) {
		c.AbsMaxHeatSetpointLimit = 32700
		c.InitialHeatingSetpoint = 32000
	})
	if _, err := srv.MatterInvoke(context.Background(), tdef.CmdSetpointRaiseLower,
		tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 127}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tdef.AttrOccupiedHeatingSetpoint); v != int16(32700) {
		t.Fatalf("OccupiedHeatingSetpoint %v after +12.7 °C from 320.00 °C, want the 327.00 °C limit", v)
	}
}
