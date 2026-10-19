// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/PresetsConfigurationTest.ts
// at 862ad75f: the write rules without configured presets, and presets
// the configuration supplies.
//
// The remote case runs over the wire on a commissioned fabrictest pair;
// the others on a fabrictest device (MockServerNode.createOnline), a local
// write being the server's SetPresets inside Device.Act and a read of
// `deviceEp.state.thermostat.presets` its MatterRead of Presets. matter.js
// tells a behavior type's own defaults (`PresetsThermostatServer.set`)
// from an endpoint's options; the Go server has one configuration,
// ThermostatConfig.Presets, which both cases use.
//
// Not ported: "seeds from configuration, not from presets a device stored
// before the accessor existed" — it plants a value under the attribute's
// pre-accessor storage key; the Go server never stored presets anywhere
// but its PresetStore, so there is no older key to ignore.
package thermo_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// unconfiguredPresets is preset-helpers.ts:69-73 unconfiguredPresetsEndpoint:
// thermostatConfig without presets.
func unconfiguredPresets() thermo.ThermostatConfig {
	cfg := thermostatConfig(5, nil)
	cfg.Presets = nil
	return cfg
}

// onlineThermostat is MockServerNode.createOnline with the thermostat:
// a fabrictest device serving srv, not commissioned.
func onlineThermostat(t *testing.T, cfg thermo.ThermostatConfig) (*fabrictest.Site, *fabrictest.Device, *thermo.ThermostatServer) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	srv := newServer(t, site, cfg)
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{thermostatEndpoint(srv)}})
	return site, dev, srv
}

// servedPresets is the Presets attribute as the server serves it.
func servedPresets(t *testing.T, srv *thermo.ThermostatServer) []thermo.Preset {
	t.Helper()
	v, ok := srv.MatterRead(presetsAttribute)
	if !ok {
		t.Fatal("Presets not served")
	}
	return []thermo.Preset(v.(spec.List[thermo.Preset]))
}

func TestParityMatterJS_PresetsConfigurationTest(t *testing.T) {
	t.Run("declines a remote write outside an atomic write when the application configured no presets", func(t *testing.T) {
		p := commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return unconfiguredPresets() })
		ctx := parityCtx(t)

		// Presets carries quality T, so a client may only write it inside an
		// atomic write.
		statuses, err := p.writePresets(ctx, p.pair, []thermo.Preset{newPreset()})
		if err != nil {
			t.Fatal(err)
		}
		if statuses.Err() == nil {
			t.Fatalf("write outside an atomic write accepted: %+v", statuses)
		}
		if got := servedPresets(t, p.srv); len(got) != 0 {
			t.Fatalf("presets %+v, want none", got)
		}

		// The same write inside an atomic write is accepted, so the refusal
		// above is the atomicity rule rather than the endpoint failing to
		// serve the attribute at all.
		if _, err := p.atomicRequest(ctx, p.pair, beginWrite, []uint32{presetsAttribute}, timeoutMs(5000)); err != nil {
			t.Fatal(err)
		}
		statuses, err = p.writePresets(ctx, p.pair, []thermo.Preset{newPreset()})
		if err != nil || statuses.Err() != nil {
			t.Fatalf("write inside the atomic write: %+v, %v", statuses, err)
		}
		if _, err := p.atomicRequest(ctx, p.pair, commitWrite, []uint32{presetsAttribute}, nil); err != nil {
			t.Fatal(err)
		}
		got := servedPresets(t, p.srv)
		if len(got) != 1 || got[0].PresetHandle.Null || len(got[0].PresetHandle.Value) != 16 {
			t.Errorf("presets %+v, want one with a 16-byte handle", got)
		}
	})

	t.Run("validates a local write when the application configured no presets", func(t *testing.T) {
		_, dev, srv := onlineThermostat(t, unconfiguredPresets())
		err := act(t, dev, func(ctx context.Context) error {
			return srv.SetPresets(ctx, []thermo.Preset{newPreset(withHandle([]byte{1, 2, 3, 4}))})
		})
		wantStatusCode(t, err, im.StatusNotFound, "does not exist in old Presets")

		if err := act(t, dev, func(ctx context.Context) error { return srv.SetPresets(ctx, []thermo.Preset{newPreset()}) }); err != nil {
			t.Fatal(err)
		}
		if got := servedPresets(t, srv); len(got) != 1 || len(got[0].PresetHandle.Value) != 16 {
			t.Errorf("presets %+v, want one with a 16-byte handle", got)
		}
	})

	t.Run("serves presets a behavior type supplies as its own default", func(t *testing.T) {
		preset := newPreset(withHandle(filled(16, 9)))
		_, _, srv := onlineThermostat(t, thermostatConfig(5, []thermo.Preset{preset}))
		got := servedPresets(t, srv)
		if len(got) != 1 || !bytes.Equal(got[0].PresetHandle.Value, preset.PresetHandle.Value) {
			t.Errorf("presets %+v, want the configured one with its handle", got)
		}
	})

	t.Run("leaves the presets a behavior type supplies untouched", func(t *testing.T) {
		preset := newPreset()
		cfg := thermostatConfig(5, []thermo.Preset{preset})
		_, _, srv := onlineThermostat(t, cfg)
		// The configuration is the caller's, and validation issues handles
		// into the presets it is given — into a copy.
		if !preset.PresetHandle.Null || !cfg.Presets[0].PresetHandle.Null {
			t.Errorf("configured preset given a handle: %+v", cfg.Presets[0])
		}
		if got := servedPresets(t, srv); len(got) != 1 || len(got[0].PresetHandle.Value) != 16 {
			t.Errorf("presets %+v, want one with an issued handle", got)
		}
	})
}
