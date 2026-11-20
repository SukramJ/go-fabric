// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestThermostatSuggestionStoreRoundTrip: on the SQLite store and on
// memstore, nothing stored reads as not ok; a save reads back as written,
// per endpoint, a nil override as nil and an empty list as empty; a second
// save replaces the first; an erase takes it along.
func TestThermostatSuggestionStoreRoundTrip(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]interface {
		store.Settings
		store.Eraser
	}{
		"sqlite":   store.New(openTestDB(t)),
		"memstore": memstore.New(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			p := store.NewThermostatSuggestionStore(s, "thermostat")
			if b, o, ok, err := p.LoadSuggestions(ctx); err != nil || ok || b != nil || o != nil {
				t.Fatalf("empty store: %x %v %v %v", b, o, ok, err)
			}
			id := uint8(7)
			if err := p.SaveSuggestions(ctx, []byte{0x16, 0x18}, &id); err != nil {
				t.Fatal(err)
			}
			other := store.NewThermostatSuggestionStore(s, "other")
			if err := other.SaveSuggestions(ctx, nil, nil); err != nil {
				t.Fatal(err)
			}
			wantLoad(ctx, t, p, []byte{0x16, 0x18}, &id)
			if err := p.SaveSuggestions(ctx, []byte{0x16, 0x15, 0x18, 0x18}, nil); err != nil {
				t.Fatal(err)
			}
			wantLoad(ctx, t, p, []byte{0x16, 0x15, 0x18, 0x18}, nil)
			wantLoad(ctx, t, other, []byte{}, nil)
			if err := s.Erase(ctx); err != nil {
				t.Fatal(err)
			}
			if _, _, ok, err := p.LoadSuggestions(ctx); err != nil || ok {
				t.Fatalf("after erase: ok %v, err %v", ok, err)
			}
		})
	}
}

func wantLoad(ctx context.Context, t *testing.T, p *store.ThermostatSuggestionStore, list []byte, overridden *uint8) {
	t.Helper()
	b, o, ok, err := p.LoadSuggestions(ctx)
	if err != nil || !ok || !bytes.Equal(b, list) || (b == nil) != (list == nil) {
		t.Fatalf("load: %x %v %v, want %x", b, ok, err, list)
	}
	if (o == nil) != (overridden == nil) || (o != nil && *o != *overridden) {
		t.Fatalf("load override %v, want %v", o, overridden)
	}
}
