// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LoadThermostatPresets returns what SaveThermostatPresets stored for the
// Thermostat endpoint endpointKey: the TLV encoding of its Presets list and
// its active preset handle (nil for none). ok is false when nothing was
// stored.
func (s *Store) LoadThermostatPresets(ctx context.Context, endpointKey string) (presets, activeHandle []byte, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT presets, active_preset_handle FROM matter_thermostat_presets WHERE endpoint_key = ?`, endpointKey).Scan(&presets, &activeHandle)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, false, nil
	case err != nil:
		return nil, nil, false, fmt.Errorf("store: load thermostat presets: %w", err)
	}
	if presets == nil {
		presets = []byte{}
	}
	return presets, activeHandle, true, nil
}

// SaveThermostatPresets replaces what is stored for the Thermostat endpoint
// endpointKey: presets is the TLV encoding of its Presets list, activeHandle
// its active preset handle (nil stores NULL, no active preset).
func (s *Store) SaveThermostatPresets(ctx context.Context, endpointKey string, presets, activeHandle []byte) error {
	if presets == nil {
		presets = []byte{}
	}
	var active any
	if activeHandle != nil {
		active = activeHandle
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_thermostat_presets (endpoint_key, presets, active_preset_handle, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(endpoint_key) DO UPDATE SET
    presets = excluded.presets,
    active_preset_handle = excluded.active_preset_handle,
    updated_at = excluded.updated_at`, endpointKey, presets, active); err != nil {
		return fmt.Errorf("store: save thermostat presets: %w", err)
	}
	return nil
}

// ThermostatPresetStore is the store of one Thermostat endpoint's presets
// — the thermo.PresetStore a host hands the Thermostat server
// (thermo.ThermostatConfig.PresetStore).
type ThermostatPresetStore struct {
	s   ThermostatPresets
	key string
}

// NewThermostatPresetStore returns the preset store of the Thermostat endpoint
// endpointKey (the host's name for it — its StableKey) on s, any
// implementation of the [ThermostatPresets] area.
func NewThermostatPresetStore(s ThermostatPresets, endpointKey string) *ThermostatPresetStore {
	return &ThermostatPresetStore{s: s, key: endpointKey}
}

// LoadPresets is [ThermostatPresets.LoadThermostatPresets] for the store's endpoint.
func (p *ThermostatPresetStore) LoadPresets(ctx context.Context) (presets, activeHandle []byte, ok bool, err error) {
	return p.s.LoadThermostatPresets(ctx, p.key)
}

// SavePresets is [ThermostatPresets.SaveThermostatPresets] for the store's endpoint.
func (p *ThermostatPresetStore) SavePresets(ctx context.Context, presets, activeHandle []byte) error {
	return p.s.SaveThermostatPresets(ctx, p.key, presets, activeHandle)
}
