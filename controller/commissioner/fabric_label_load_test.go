// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"thingfabric.dev/go-fabric/controller/ca"
)

// storeWithLabel is a store whose fabric record carries label, as a
// controller before v0.25 left it: v0.22.2's SetLabel bounded the label
// in runes, so a label of 32 runes and up to 128 bytes was stored.
func storeWithLabel(t *testing.T, label string) (*ca.CA, *ca.MemoryStore) {
	t.Helper()
	st := &ca.MemoryStore{}
	c, err := ca.New(ca.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFabric(c, FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := st.LoadFabric()
	rec.Label = label
	if err := st.SaveFabric(rec); err != nil {
		t.Fatal(err)
	}
	return c, st
}

// TestStoredFabricLabelOverTheBoundIsCutAtLoad: a stored fabric label over
// MaxLabelLength bytes — one v0.22.2 stored while SetLabel counted runes —
// loads, cut to the bound between two characters with a warning, as
// cluster/core BasicInformation.RestoreNodeLabel cuts a stored NodeLabel
// (go-fabric #587, decision 557). The cut stays in memory until the next
// save; NewFabric, CreateFabric and SetLabel keep refusing a longer label.
func TestStoredFabricLabelOverTheBoundIsCutAtLoad(t *testing.T) {
	t.Parallel()
	cases := []struct{ stored, want string }{
		{strings.Repeat("ü", 32), strings.Repeat("ü", 16)},             // 32 runes, 64 bytes: v0.22.2's bound
		{"a" + strings.Repeat("ü", 16), "a" + strings.Repeat("ü", 15)}, // 33 bytes: the cut steps back off a rune
		{strings.Repeat("x", 31) + "😀", strings.Repeat("x", 31)},       // the 4-byte rune at the bound goes whole
		{strings.Repeat("ü", 16), strings.Repeat("ü", 16)},             // 32 bytes: untouched
		{strings.Repeat("中", 32), strings.Repeat("中", 10)},             // 96 bytes: 30 bytes on a rune boundary
		{strings.Repeat("x", 32) + "y", strings.Repeat("x", 32)},       // 33 ASCII bytes
		{"", ""}, // no label
		{strings.Repeat("😀", 32), strings.Repeat("😀", 8)},                    // 128 bytes
		{strings.Repeat("ä", 17), strings.Repeat("ä", 16)},                   // the 17 umlauts of the issue
		{strings.Repeat("x", 30) + "ü" + "x", strings.Repeat("x", 30) + "ü"}, // 33 bytes, the bound right after a rune
	}
	for _, tc := range cases {
		c, st := storeWithLabel(t, tc.stored)
		f, err := NewFabric(c, FabricConfig{Store: st})
		if err != nil {
			t.Fatalf("stored label of %d bytes: %v, want it loaded", len(tc.stored), err)
		}
		got := f.CurrentLabel()
		if got != tc.want || len(got) > MaxLabelLength || !utf8.ValidString(got) {
			t.Errorf("stored label %q (%d bytes): served %q (%d bytes), want %q", tc.stored, len(tc.stored), got, len(got), tc.want)
		}
		// In memory only: the load writes nothing; the record keeps the
		// stored label until the next save.
		if rec, _, _ := st.LoadFabric(); rec.Label != tc.stored {
			t.Errorf("stored label %q: the load rewrote the record's label to %q", tc.stored, rec.Label)
		}
		// SetLabel keeps refusing a label over the bound.
		if err := f.SetLabel(strings.Repeat("x", 33)); !errors.Is(err, ErrLabel) {
			t.Errorf("SetLabel with 33 bytes after the load: %v, want ErrLabel", err)
		}
		// The next save the package does carries the cut label.
		if err := f.SetLabel("renamed"); err != nil {
			t.Fatal(err)
		}
		if rec, _, _ := st.LoadFabric(); rec.Label != "renamed" {
			t.Errorf("SetLabel after the load saved label %q, want %q", rec.Label, "renamed")
		}
	}
}

// TestStoredFabricLabelCutIsWarnedAndSavedWithTheRotation: the authority's
// DefaultFabric warns through its logger when it cut a stored label, and
// the NOC rotation — a save the package already does — persists the cut.
func TestStoredFabricLabelCutIsWarnedAndSavedWithTheRotation(t *testing.T) {
	t.Parallel()
	stored := strings.Repeat("ü", 32)
	c, st := storeWithLabel(t, stored)
	var out syncBuffer
	a := NewFabricAuthority(c, slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelWarn})))
	f, err := a.DefaultFabric(FabricConfig{Store: st}, true)
	if err != nil {
		t.Fatalf("DefaultFabric over a stored label of %d bytes: %v", len(stored), err)
	}
	want := strings.Repeat("ü", 16)
	if got := f.CurrentLabel(); got != want {
		t.Errorf("served label %q, want %q", got, want)
	}
	if rec, _, _ := st.LoadFabric(); rec.Label != want {
		t.Errorf("the rotation saved label %q, want the cut %q", rec.Label, want)
	}
	log := out.String()
	if !strings.Contains(log, "level=WARN") || !strings.Contains(log, "label") || !strings.Contains(log, "max_bytes=32") {
		t.Errorf("no warning of the cut through the authority's logger; log:\n%s", log)
	}
	if _, err := a.DefaultFabric(FabricConfig{Store: st, Label: strings.Repeat("x", 33)}, false); !errors.Is(err, ErrLabel) {
		t.Errorf("DefaultFabric with a configured label of 33 bytes: %v, want ErrLabel", err)
	}
}
