// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
)

// holdSink is a thermostatdef.Sink that records the holds it is told and
// refuses each with err.
type holdSink struct {
	mu    sync.Mutex
	holds []uint8
	err   error
}

func (*holdSink) SetSystemMode(context.Context, uint8) error { return nil }

func (*holdSink) SetSetpoint(context.Context, uint32, int16, []thermostatdef.SetpointChange) error {
	return nil
}

func (h *holdSink) SetTemperatureSetpointHold(_ context.Context, hold uint8) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holds = append(h.holds, hold)
	return h.err
}

// TestThermostatDeclaresHoldAndRunningState: a declaration's Initial that
// sets TemperatureSetpointHold and ThermostatRunningState serves them on
// the kit's Thermostat server — with the hold also
// TemperatureSetpointHoldDuration, null (matter.js ThermostatServer.ts:365-369
// at 862ad75f) — and a controller's hold write reaches the declared Sink,
// whose refusal refuses the write with its status. A declaration that
// sets neither serves neither.
func TestThermostatDeclaresHoldAndRunningState(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: device.StringKey(key), Name: key} }
	sink := &holdSink{}
	off, heating := thermostatdef.TemperatureSetpointHoldSetpointHoldOff, thermostatdef.RelayStateHeat
	d, err := device.NewThermostat(opts("thermo-hold"), thermostatdef.Declare{
		Features: thermostatdef.FeatureHeating,
		Initial:  thermostatdef.Patch{TemperatureSetpointHold: &off, ThermostatRunningState: &heating},
		Sink:     sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	for _, id := range []uint32{
		thermostatdef.AttrTemperatureSetpointHold, thermostatdef.AttrTemperatureSetpointHoldDuration,
		thermostatdef.AttrThermostatRunningState,
	} {
		if _, ok := srv.Underlying().MatterRead(id); !ok {
			t.Errorf("attribute 0x%04X not served", id)
		}
	}
	st := srv.Get()
	if st.TemperatureSetpointHold != off || st.ThermostatRunningState != heating || st.TemperatureSetpointHoldDuration != nil {
		t.Errorf("state hold %v running %v duration %v, want %v / %v / null",
			st.TemperatureSetpointHold, st.ThermostatRunningState, st.TemperatureSetpointHoldDuration, off, heating)
	}

	ctx := context.Background()
	if err := srv.Underlying().MatterWrite(ctx, thermostatdef.AttrTemperatureSetpointHold, uint8(1)); err != nil {
		t.Fatal(err)
	}
	sink.err = spec.Errorf(im.StatusBusy, "device busy")
	err = srv.Underlying().MatterWrite(ctx, thermostatdef.AttrTemperatureSetpointHold, uint8(0))
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusBusy {
		t.Errorf("refused hold write: %v, want BUSY", err)
	}
	if got := srv.Get().TemperatureSetpointHold; got != thermostatdef.TemperatureSetpointHoldSetpointHoldOn {
		t.Errorf("TemperatureSetpointHold = %v after the refusal, want SetpointHoldOn", got)
	}
	if !slices.Equal(sink.holds, []uint8{1, 0}) {
		t.Errorf("Sink holds %v, want [1 0]", sink.holds)
	}

	plain, err := device.NewThermostat(opts("thermo-plain"), thermostatdef.Declare{Features: thermostatdef.FeatureHeating})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Thermostat().Underlying().MatterRead(thermostatdef.AttrTemperatureSetpointHold); ok {
		t.Error("TemperatureSetpointHold served without the declaration setting it")
	}
}
