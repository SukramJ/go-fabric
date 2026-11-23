// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/thermo"
)

// failOncePresetStore refuses the next save once armed, and keeps every
// save it accepts.
type failOncePresetStore struct {
	mu              sync.Mutex
	presets, active []byte
	saved           bool
	failNext        bool
}

func (m *failOncePresetStore) LoadPresets(context.Context) (presets, activeHandle []byte, ok bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.presets, m.active, m.saved, nil
}

func (m *failOncePresetStore) SavePresets(_ context.Context, presets, active []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext {
		m.failNext = false
		return errors.New("store unavailable")
	}
	m.presets, m.active, m.saved = presets, active, true
	return nil
}

// TestClearedActivePresetReachesTheStoreAfterAFailedSave
// (R3-store-3253): a direct setpoint change ends the active preset in
// memory even when the PresetStore refuses that save; the next direct
// change saves the pair again, so the store does not keep the ended
// handle and a restart does not bring the preset back. Before the fix the
// next change found no active preset and saved nothing.
func TestClearedActivePresetReachesTheStoreAfterAFailedSave(t *testing.T) {
	t.Parallel()
	ps := &failOncePresetStore{}
	cfg := thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2000, 2600)})
	cfg.PresetStore = ps
	srv := mustNew(t, cfg)
	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	ps.mu.Lock()
	ps.failNext = true
	ps.mu.Unlock()
	srv.SetOccupiedHeatingSetpoint(1900) // ends the preset; its save fails
	if h := srv.ActivePresetHandle(); h != nil {
		t.Fatalf("active handle %x after a direct setpoint change, want none", h)
	}
	srv.SetOccupiedHeatingSetpoint(1800) // a further change
	ps.mu.Lock()
	active := ps.active
	ps.mu.Unlock()
	if active != nil {
		t.Errorf("store holds active handle %x, memory none", active)
	}
	if h := mustNew(t, cfg).ActivePresetHandle(); h != nil {
		t.Errorf("after a restart the ended preset %x is active again", h)
	}
}
