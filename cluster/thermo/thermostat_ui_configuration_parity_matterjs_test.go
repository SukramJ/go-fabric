// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat-user-interface-configuration/ThermostatUserInterfaceConfgurationServerTest.ts
// at 862ad75f: the server instantiates, KeypadLockout defaults to
// NoLockout, keeps a configured value and keeps a written one across a
// restart.
//
// matter.js runs the cases on MockServerNodes; the port runs fabrictest
// devices. The restart case adds the server to the running node with
// Settings on the node's own store (Aggregator.Add inside Device.Act),
// writes locally (`device.set` is the server's Set), restarts the device
// and adds a new server on the restarted node's store.
package thermo_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// The device types of the cases (room-air-conditioner.element.ts,
// temperature-sensor.element.ts).
const (
	deviceTypeRoomAirConditioner = 0x0072
	deviceTypeTemperatureSensor  = 0x0302
)

// keypadLockoutOf reads KeypadLockout.
func keypadLockoutOf(t *testing.T, srv *thermo.ThermostatUIServer) thermo.KeypadLockout {
	t.Helper()
	v, ok := srv.MatterRead(tsuic.AttrKeypadLockout)
	if !ok {
		t.Fatal("KeypadLockout not served")
	}
	k, ok := cluster.AsUint8(v)
	if !ok {
		t.Fatalf("KeypadLockout reads %T", v)
	}
	return thermo.KeypadLockout(k)
}

// uiSensor is ThermostatUserInterfaceConfgurationServerTest.ts:17-22: a
// temperature sensor with the server, KeypadLockout as configured.
func uiSensor(t *testing.T, keypadLockout *thermo.KeypadLockout) *thermo.ThermostatUIServer {
	t.Helper()
	srv, err := thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{KeypadLockout: keypadLockout})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("sensor", deviceTypeTemperatureSensor, srv)}})
	return srv
}

func TestParityMatterJS_ThermostatUserInterfaceConfigurationServerTest(t *testing.T) {
	t.Run("instantiates", func(t *testing.T) {
		srv, err := thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{})
		if err != nil {
			t.Fatal(err)
		}
		site := fabrictest.NewSite(t, fabrictest.Options{})
		site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("aircon", deviceTypeRoomAirConditioner, srv)}})
	})

	t.Run("defaults KeypadLockout to NoLockout", func(t *testing.T) {
		if got := keypadLockoutOf(t, uiSensor(t, nil)); got != tsuic.KeypadLockoutNoLockout {
			t.Errorf("keypadLockout %d, want NoLockout", got)
		}
	})

	t.Run("keeps a configured KeypadLockout", func(t *testing.T) {
		lockout2 := tsuic.KeypadLockoutLockout2
		if got := keypadLockoutOf(t, uiSensor(t, &lockout2)); got != tsuic.KeypadLockoutLockout2 {
			t.Errorf("keypadLockout %d, want Lockout2", got)
		}
	})

	t.Run("keeps a written KeypadLockout across a restart", func(t *testing.T) {
		site := fabrictest.NewSite(t, fabrictest.Options{})
		dev := site.AddDevice(fabrictest.Topology{})
		addSensor := func() *thermo.ThermostatUIServer {
			var srv *thermo.ThermostatUIServer
			if err := dev.Act(func(n *node.Node) error {
				var err error
				srv, err = thermo.NewThermostatUserInterfaceConfiguration(thermo.ThermostatUIConfig{Settings: n.Store()})
				if err != nil {
					return err
				}
				return n.Aggregator().Add(context.Background(), fabrictest.Endpoint("sensor", deviceTypeTemperatureSensor, srv))
			}); err != nil {
				t.Fatal(err)
			}
			return srv
		}

		first := addSensor()
		if err := act(t, dev, func(context.Context) error { return first.Set(tsuic.AttrKeypadLockout, tsuic.KeypadLockoutLockout5) }); err != nil {
			t.Fatal(err)
		}
		if err := dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := keypadLockoutOf(t, addSensor()); got != tsuic.KeypadLockoutLockout5 {
			t.Errorf("keypadLockout after the restart %d, want Lockout5", got)
		}
	})
}
