// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"testing"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// nullSink is a Thermostat Sink that takes every change.
type nullSink struct{}

func (nullSink) SetSystemMode(context.Context, uint8) error { return nil }

func (nullSink) SetSetpoint(context.Context, uint32, int16, []thermostatdef.SetpointChange) error {
	return nil
}

func (nullSink) SetTemperatureSetpointHold(context.Context, uint8) error { return nil }

// TestHoldsAttribute: the device holds an attribute of a server it built
// itself unless a declared host port takes the attribute's writes; a host
// server's attributes and a cluster it does not mount are not its own
// (R2-P2-store-04).
func TestHoldsAttribute(t *testing.T) {
	t.Parallel()
	heat := thermostatdef.Declare{Features: thermostatdef.FeatureHeating}
	held, err := NewThermostat(testOptions("held"), heat)
	if err != nil {
		t.Fatal(err)
	}
	sinkDecl := heat
	sinkDecl.Sink = nullSink{}
	sunk, err := NewThermostat(testOptions("sunk"), sinkDecl)
	if err != nil {
		t.Fatal(err)
	}
	hosted, err := NewOnOffLight(testOptions("hosted"), HostServer(&hostServer{id: onoffdef.ClusterID}))
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		d       *Device
		cluster uint32
		attr    uint32
		want    bool
	}{
		"setpoint without a Sink":   {held.Device, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint, true},
		"setpoint with a Sink":      {sunk.Device, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint, false},
		"SystemMode with a Sink":    {sunk.Device, thermostatdef.ClusterID, thermostatdef.AttrSystemMode, false},
		"limit with a Sink":         {sunk.Device, thermostatdef.ClusterID, thermostatdef.AttrMinHeatSetpointLimit, true},
		"a host server's":           {hosted.Device, onoffdef.ClusterID, onoffdef.AttrStartUpOnOff, false},
		"a cluster it never mounts": {held.Device, onoffdef.ClusterID, onoffdef.AttrStartUpOnOff, false},
	} {
		if got := tc.d.HoldsAttribute(tc.cluster, tc.attr); got != tc.want {
			t.Errorf("%s: HoldsAttribute %v, want %v", name, got, tc.want)
		}
	}
}
