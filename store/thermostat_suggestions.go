// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
)

// The settings a ThermostatSuggestionStore keeps one endpoint's
// suggestions under: the root and the endpoint's key. The list is the hex
// of its TLV encoding, the override its UniqueID in decimal, "" for none.
const (
	thermostatSuggestionsKeyRoot = "thermostat.suggestions."
	thermostatOverrideKeyRoot    = "thermostat.overridden_suggestion."
)

// ThermostatSuggestionStore is the store of one Thermostat endpoint's
// thermostat suggestions and manual override — the thermo.SuggestionStore
// a host hands the Thermostat server (thermo.ThermostatConfig.SuggestionStore).
// It keeps them as two settings, written in one atomic SetSettings, so
// any implementation of the [Settings] area holds them without a table of
// its own, and an erase of the store takes them along.
type ThermostatSuggestionStore struct {
	s   Settings
	key string
}

// NewThermostatSuggestionStore returns the suggestion store of the
// Thermostat endpoint endpointKey (the host's name for it — its
// StableKey, the key its [ThermostatPresetStore] uses) on s.
func NewThermostatSuggestionStore(s Settings, endpointKey string) *ThermostatSuggestionStore {
	return &ThermostatSuggestionStore{s: s, key: endpointKey}
}

// LoadSuggestions returns what SaveSuggestions stored: the TLV encoding
// of the ThermostatSuggestions list and the overridden entry's UniqueID
// (nil for none). ok is false when nothing was stored.
func (p *ThermostatSuggestionStore) LoadSuggestions(ctx context.Context) (suggestions []byte, overridden *uint8, ok bool, err error) {
	list, ok, err := p.s.GetSetting(ctx, thermostatSuggestionsKeyRoot+p.key)
	if err != nil || !ok {
		return nil, nil, false, err
	}
	if suggestions, err = hex.DecodeString(list); err != nil {
		return nil, nil, false, fmt.Errorf("store: load thermostat suggestions: %w", err)
	}
	id, _, err := p.s.GetSetting(ctx, thermostatOverrideKeyRoot+p.key)
	if err != nil {
		return nil, nil, false, err
	}
	if id != "" {
		n, err := strconv.ParseUint(id, 10, 8)
		if err != nil {
			return nil, nil, false, fmt.Errorf("store: load thermostat suggestion override: %w", err)
		}
		v := uint8(n)
		overridden = &v
	}
	return suggestions, overridden, true, nil
}

// SaveSuggestions replaces what is stored: suggestions is the TLV encoding
// of the ThermostatSuggestions list, overridden the UniqueID of the entry
// a manual override holds (nil for none).
func (p *ThermostatSuggestionStore) SaveSuggestions(ctx context.Context, suggestions []byte, overridden *uint8) error {
	id := ""
	if overridden != nil {
		id = strconv.Itoa(int(*overridden))
	}
	return p.s.SetSettings(ctx, map[string]string{
		thermostatSuggestionsKeyRoot + p.key: hex.EncodeToString(suggestions),
		thermostatOverrideKeyRoot + p.key:    id,
	})
}
