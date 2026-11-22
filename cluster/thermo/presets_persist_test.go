// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"testing/synctest"

	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
)

// gatedPresetStore records every save; when armed, the next save waits for
// release before it returns, as a slow store would.
type gatedPresetStore struct {
	mu              sync.Mutex
	presets, active []byte
	saved           bool
	armed           bool
	entered         chan struct{}
	release         chan struct{}
}

func (m *gatedPresetStore) LoadPresets(context.Context) (presets, activeHandle []byte, ok bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.presets, m.active, m.saved, nil
}

func (m *gatedPresetStore) SavePresets(_ context.Context, presets, active []byte) error {
	m.mu.Lock()
	m.presets, m.active, m.saved = presets, active, true
	block := m.armed
	m.armed = false
	m.mu.Unlock()
	if block {
		close(m.entered)
		<-m.release
	}
	return nil
}

func (m *gatedPresetStore) arm() {
	m.mu.Lock()
	m.armed = true
	m.mu.Unlock()
}

// TestPresetStoreHoldsThePairAfterAConcurrentClear (R2-P6-store-1501): a
// host's direct setpoint change that ends the active preset, landing while
// a preset write or an active-handle write is saving, must not overwrite
// the store with the pair it read before that write. Each save writes the
// pair current when it runs, so after both the store holds what memory
// holds, and a restart serves it.
func TestPresetStoreHoldsThePairAfterAConcurrentClear(t *testing.T) {
	t.Parallel()
	second := func(heat int16) thermo.Preset {
		p := setpointPreset(2, heat, 2700)
		p.PresetScenario = tdef.PresetScenarioUnoccupied
		return p
	}
	cases := []struct {
		name  string
		write func(*thermo.ThermostatServer) error
	}{
		{"SetPresets", func(srv *thermo.ThermostatServer) error {
			return srv.SetPresets(context.Background(), []thermo.Preset{setpointPreset(1, 2100, 2600), second(1800)})
		}},
		{"SetActivePresetHandle", func(srv *thermo.ThermostatServer) error {
			return srv.SetActivePresetHandle(context.Background(), []byte{2})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ps := &gatedPresetStore{entered: make(chan struct{}), release: make(chan struct{})}
				cfg := thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2000, 2600), second(1800)})
				cfg.PresetTypes = append(cfg.PresetTypes, thermo.PresetType{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 5})
				cfg.PresetStore = ps
				srv := mustNew(t, cfg)
				if err := activate(t, srv, []byte{1}); err != nil {
					t.Fatal(err)
				}
				ps.arm()
				written := make(chan error, 1)
				go func() { written <- tc.write(srv) }()
				<-ps.entered
				cleared := make(chan struct{})
				go func() {
					srv.SetOccupiedHeatingSetpoint(1900) // host: a direct change ends the active preset
					close(cleared)
				}()
				synctest.Wait()
				select {
				case <-cleared:
					t.Error("the clear's save ran while the write's save was in progress")
				default:
				}
				close(ps.release)
				if err := <-written; err != nil {
					t.Fatal(err)
				}
				<-cleared

				stored, err := thermo.DecodePresets(ps.presets)
				if err != nil {
					t.Fatal(err)
				}
				mem := srv.Presets()
				if len(stored) != len(mem) || *stored[0].HeatingSetpoint != *mem[0].HeatingSetpoint {
					t.Errorf("store holds %d presets, preset 1 heating %d; memory %d, %d", len(stored), *stored[0].HeatingSetpoint, len(mem), *mem[0].HeatingSetpoint)
				}
				if !bytes.Equal(ps.active, srv.ActivePresetHandle()) {
					t.Errorf("store holds active %x, memory %x", ps.active, srv.ActivePresetHandle())
				}
				restarted := mustNew(t, cfg)
				if got := *restarted.Presets()[0].HeatingSetpoint; got != *mem[0].HeatingSetpoint {
					t.Errorf("after a restart preset 1 heats to %d, before it %d", got, *mem[0].HeatingSetpoint)
				}
				if !bytes.Equal(restarted.ActivePresetHandle(), srv.ActivePresetHandle()) {
					t.Errorf("after a restart the active handle is %x, before it %x", restarted.ActivePresetHandle(), srv.ActivePresetHandle())
				}
			})
		})
	}
}
