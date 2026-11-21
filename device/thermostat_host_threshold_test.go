// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"sync"
	"testing"

	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/surface"
)

// localTempEvents counts the LocalTemperatureChange events a mounted
// Thermostat emits.
type localTempEvents struct {
	mu sync.Mutex
	n  int
}

func (e *localTempEvents) MatterEmitEvent(_ uint16, _, eventID uint32, _ any, _ contract.EventPriority) {
	if eventID != thermostatdef.EventLocalTemperatureChange {
		return
	}
	e.mu.Lock()
	e.n++
	e.mu.Unlock()
}

func (e *localTempEvents) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n
}

// newThermostatHost builds the kit's Thermostat with features, mounted as
// the bridge mounts it (its emitter, then its endpoint) after the host
// measured start, and returns its Host and the event counter.
func newThermostatHost(t *testing.T, key string, features thermostatdef.Feature, start int16) (thermostatdef.Host, *localTempEvents) {
	t.Helper()
	// TEVT is the ahead-of-matter.js row BD-chip-Thermostat-Events.
	opts := device.Options{StableKey: device.StringKey(key), Name: "thermo", Surface: surface.Policies{Extensions: surface.NewExtensions("BD-chip-Thermostat-Events")}}
	d, err := device.NewThermostat(opts, thermostatdef.Declare{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	host := srv.Host()
	if host == nil {
		t.Fatal("the kit's Thermostat has no Host")
	}
	host.SetMeasuredTemperature(&start) // before the mount: the reported value, no event
	em := &localTempEvents{}
	under := srv.Underlying()
	r, ok := under.(contract.EventReceiver)
	if !ok {
		t.Fatalf("%T takes no event emitter", under)
	}
	r.SetMatterEventEmitter(em)
	ep, ok := under.(contract.EndpointReceiver)
	if !ok {
		t.Fatalf("%T takes no endpoint", under)
	}
	ep.SetEndpoint(5)
	return host, em
}

// TestThermostatHostLocalTemperatureChangeThreshold: the device facade
// reaches the kit's SetLocalTemperatureChangeThreshold through the typed
// server's Host (owner decision 2026-11-21). The threshold decides which
// change of LocalTemperature the LocalTemperatureChange event reports;
// with the event (TEVT without LTNE) 0 is refused, without it accepted.
// Mirrors matter.js ThermostatServer.ts:2195-2196 at b25ed960
// (the threshold checked on every change) and
// ThermostatServer.ts:2202-2208 at b25ed960
// (#assertLocalTemperatureChangeThreshold).
func TestThermostatHostLocalTemperatureChangeThreshold(t *testing.T) {
	t.Parallel()
	const start = int16(2000)

	t.Run("the threshold decides which change is reported", func(t *testing.T) {
		t.Parallel()
		host, em := newThermostatHost(t, "thermo-host-threshold", thermostatdef.FeatureHeating|thermostatdef.FeatureEvents, start)
		if err := host.SetLocalTemperatureChangeThreshold(200); err != nil {
			t.Fatal(err)
		}
		below := start + 100 // significant with the default 50, not with 200
		host.SetMeasuredTemperature(&below)
		if n := em.count(); n != 0 {
			t.Fatalf("%d LocalTemperatureChange events for a change of 100 under a threshold of 200, want 0", n)
		}
		at := start + 200
		host.SetMeasuredTemperature(&at)
		if n := em.count(); n != 1 {
			t.Fatalf("%d LocalTemperatureChange events for a change of 200 under a threshold of 200, want 1", n)
		}
	})

	t.Run("0 is refused with the event, the threshold kept", func(t *testing.T) {
		t.Parallel()
		host, em := newThermostatHost(t, "thermo-host-threshold-zero", thermostatdef.FeatureHeating|thermostatdef.FeatureEvents, start)
		if err := host.SetLocalTemperatureChangeThreshold(0); err == nil {
			t.Fatal("a threshold of 0 with the event accepted, want refused")
		}
		at := start + 50 // the default threshold still in effect
		host.SetMeasuredTemperature(&at)
		if n := em.count(); n != 1 {
			t.Fatalf("%d LocalTemperatureChange events for a change of 50 after the refusal, want 1 (the default 50 kept)", n)
		}
	})

	t.Run("0 is accepted without the event", func(t *testing.T) {
		t.Parallel()
		host, _ := newThermostatHost(t, "thermo-host-threshold-noevent", thermostatdef.FeatureHeating, start)
		if err := host.SetLocalTemperatureChangeThreshold(0); err != nil {
			t.Fatalf("a threshold of 0 without TEVT: %v, want accepted", err)
		}
	})
}
