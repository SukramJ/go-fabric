// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestThermostatPresetsRoundTrip: nothing stored reads as not ok; a save
// reads back as written, per endpoint, a nil active handle as nil; a second
// save replaces the first.
func TestThermostatPresetsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := store.New(openTestDB(t))
	if _, _, ok, err := s.LoadThermostatPresets(ctx, "thermostat"); err != nil || ok {
		t.Fatalf("empty store: ok %v, err %v", ok, err)
	}
	p := store.NewThermostatPresetStore(s, "thermostat")
	if err := p.SavePresets(ctx, []byte{0x16, 0x18}, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThermostatPresets(ctx, "other", nil, nil); err != nil {
		t.Fatal(err)
	}
	presets, active, ok, err := p.LoadPresets(ctx)
	if err != nil || !ok || !bytes.Equal(presets, []byte{0x16, 0x18}) || !bytes.Equal(active, []byte{1, 2}) {
		t.Fatalf("load: %x %x %v %v", presets, active, ok, err)
	}
	if err := p.SavePresets(ctx, []byte{0x16, 0x15, 0x18, 0x18}, nil); err != nil {
		t.Fatal(err)
	}
	presets, active, ok, err = p.LoadPresets(ctx)
	if err != nil || !ok || !bytes.Equal(presets, []byte{0x16, 0x15, 0x18, 0x18}) || active != nil {
		t.Fatalf("load after replace: %x %x %v %v", presets, active, ok, err)
	}
	presets, active, ok, err = s.LoadThermostatPresets(ctx, "other")
	if err != nil || !ok || len(presets) != 0 || active != nil {
		t.Fatalf("other endpoint: %x %x %v %v", presets, active, ok, err)
	}
}
