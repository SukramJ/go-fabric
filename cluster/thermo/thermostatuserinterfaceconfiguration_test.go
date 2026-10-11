// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// recordingSink records the controller writes the server hands over.
type recordingSink struct {
	writes map[uint32]any
	err    error
}

func (s *recordingSink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	if s.err != nil {
		return s.err
	}
	if s.writes == nil {
		s.writes = map[uint32]any{}
	}
	s.writes[attrID] = value
	return nil
}

func newTSUIC(t *testing.T, cfg thermo.ThermostatUIConfig) *thermo.ThermostatUIServer {
	t.Helper()
	srv, err := thermo.NewThermostatUserInterfaceConfiguration(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestParityMatterJS_ThermostatUIServer holds both shapes of the server —
// with and without the optional ScheduleProgrammingVisibility — against
// the matter.js conformance of the definition (spectest.CheckServer), and
// pins the snapshot facts the server rests on: no features, the three
// attributes with their access, and the Thermostat device type offering
// the cluster.
func TestParityMatterJS_ThermostatUIServer(t *testing.T) {
	t.Parallel()
	spectest.CheckServer(t, newTSUIC(t, thermo.ThermostatUIConfig{}), tsuic.Definition, 0)
	denied := tsuic.ScheduleProgrammingVisibilityScheduleProgrammingDenied
	srv := newTSUIC(t, thermo.ThermostatUIConfig{ScheduleProgrammingVisibility: &denied})
	spectest.CheckServer(t, srv, tsuic.Definition, 0)
	if !srv.Serves(tsuic.AttrScheduleProgrammingVisibility) {
		t.Error("ScheduleProgrammingVisibility not served when configured")
	}

	js := paritytest.ClusterSnapshot(t, thermo.ClusterIDThermostatUserInterfaceConfiguration)
	if len(js.Features) != 0 {
		t.Errorf("features %+v", js.Features)
	}
	for name, access := range map[string]string{
		"TemperatureDisplayMode":        "RW VO",
		"KeypadLockout":                 "RW VM",
		"ScheduleProgrammingVisibility": "RW VM",
	} {
		if a := js.Attribute(t, name); a.Access != access {
			t.Errorf("%s access %q, want %q", name, a.Access, access)
		}
	}
	const thermostat = 0x0301
	if allowed, known := schema.DeviceTypeAllowsServerCluster(thermostat, thermo.ClusterIDThermostatUserInterfaceConfiguration); !allowed || !known {
		t.Error("Thermostat does not offer ThermostatUserInterfaceConfiguration")
	}
}

// TestThermostatUIInitialValues is matter.js's initialize
// (ThermostatUserInterfaceConfigurationServer.ts:16-31): Celsius unless
// the host says otherwise, and KeypadLockout NoLockout.
func TestThermostatUIInitialValues(t *testing.T) {
	t.Parallel()
	srv := newTSUIC(t, thermo.ThermostatUIConfig{})
	if v, _ := srv.MatterRead(tsuic.AttrTemperatureDisplayMode); v != uint8(thermo.TemperatureDisplayCelsius) {
		t.Errorf("TemperatureDisplayMode = %v, want Celsius", v)
	}
	if v, _ := srv.MatterRead(tsuic.AttrKeypadLockout); v != uint8(tsuic.KeypadLockoutNoLockout) {
		t.Errorf("KeypadLockout = %v, want NoLockout", v)
	}
	if _, ok := srv.MatterRead(tsuic.AttrScheduleProgrammingVisibility); ok {
		t.Error("ScheduleProgrammingVisibility read without being served")
	}
	f := thermo.TemperatureDisplayFahrenheit
	srv = newTSUIC(t, thermo.ThermostatUIConfig{TemperatureDisplayMode: &f})
	if v, _ := srv.MatterRead(tsuic.AttrTemperatureDisplayMode); v != uint8(thermo.TemperatureDisplayFahrenheit) {
		t.Errorf("TemperatureDisplayMode = %v, want Fahrenheit", v)
	}
}

// TestThermostatUIWrites holds the controller writes to the definition: a
// defined value is stored and handed to the sink, an undefined one is
// CONSTRAINT_ERROR, and a sink error refuses the write.
func TestThermostatUIWrites(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	srv := newTSUIC(t, thermo.ThermostatUIConfig{Sink: sink})
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tsuic.AttrKeypadLockout, uint64(tsuic.KeypadLockoutLockout3)); err != nil {
		t.Fatalf("KeypadLockout write: %v", err)
	}
	if v, _ := srv.MatterRead(tsuic.AttrKeypadLockout); v != uint8(tsuic.KeypadLockoutLockout3) {
		t.Errorf("KeypadLockout = %v after the write", v)
	}
	if sink.writes[tsuic.AttrKeypadLockout] != uint8(tsuic.KeypadLockoutLockout3) {
		t.Errorf("sink saw %v", sink.writes)
	}
	err := srv.MatterWrite(ctx, tsuic.AttrTemperatureDisplayMode, uint64(2))
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("TemperatureDisplayMode 2: %v, want CONSTRAINT_ERROR", err)
	}
	sink.err = errors.New("refused")
	if err := srv.MatterWrite(ctx, tsuic.AttrTemperatureDisplayMode, uint64(1)); err == nil {
		t.Error("a sink error did not refuse the write")
	}
	if v, _ := srv.MatterRead(tsuic.AttrTemperatureDisplayMode); v != uint8(thermo.TemperatureDisplayCelsius) {
		t.Errorf("TemperatureDisplayMode = %v after a refused write", v)
	}
}

// TestThermostatUIRefusesAnUndefinedInitialValue holds the host's initial
// values to the model.
func TestThermostatUIRefusesAnUndefinedInitialValue(t *testing.T) {
	t.Parallel()
	bad := thermo.TemperatureDisplayMode(7)
	if _, err := thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{TemperatureDisplayMode: &bad}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("TemperatureDisplayMode 7: %v, want ErrInvalidValue", err)
	}
}
