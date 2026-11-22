// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
)

// TestParityMatterJS_MeasuredTemperatureIsCalibrated (R2-P5-apidev-06,
// F-db021da0-21): the measured input adds LocalTemperatureCalibration × 10
// to a non-null value; the direct setter stores as given; a null measurement
// sets the attribute null; with LTNE the attribute stays null — matter.js
// ThermostatServer.ts:511-527 at db021da0: the calibration :512-514, the
// write unless LTNE :516-519 (the guard :517), a null measurement setting
// the attribute null :518 and leaving the internal value :523; the handler
// of the external measurement :497-501.
func TestParityMatterJS_MeasuredTemperatureIsCalibrated(t *testing.T) {
	t.Parallel()
	read := func(srv *thermo.ThermostatServer) any {
		v, ok := srv.MatterRead(tdef.AttrLocalTemperature)
		if !ok {
			t.Fatal("LocalTemperature not served")
		}
		return v
	}
	srv := mustNew(t, thermo.DefaultThermostatConfig())
	if err := srv.MatterWrite(context.Background(), tdef.AttrLocalTemperatureCalibration, int8(-5)); err != nil {
		t.Fatal(err)
	}
	m := int16(2150)
	srv.SetMeasuredTemperature(&m)
	if got := read(srv); got != int16(2100) {
		t.Errorf("measured 2150 with calibration -5 reads %v, want 2100", got)
	}
	srv.SetLocalTemperature(&m)
	if got := read(srv); got != int16(2150) {
		t.Errorf("the direct setter's 2150 reads %v, want it uncalibrated", got)
	}
	srv.SetMeasuredTemperature(nil)
	if got := read(srv); got != nil {
		t.Errorf("a null measurement reads %v, want null", got)
	}

	ltne, _ := tevtServer(t, featHeatTEVT|thermo.ThermostatFeatureLTNE)
	ltne.SetMeasuredTemperature(&m)
	if got := read(ltne); got != nil {
		t.Errorf("a measurement under LTNE reads %v, want null", got)
	}
}
