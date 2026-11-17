// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestAnalyzerSeedsInitializersAndListsMethods runs the analyzer over the
// fixture module under testdata/fixture: a function reached only from a
// package-level var's initializer reads as reached (R2-P6-test-2805), and
// an exported method no test calls is listed as unreached (R2-P6-test-2806).
func TestAnalyzerSeedsInitializersAndListsMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("loads and analyses a module")
	}
	out := t.TempDir()
	invPath, sumPath := filepath.Join(out, "inventory.json"), filepath.Join(out, "summary.md")
	if err := run(slog.New(slog.DiscardHandler), filepath.Join("testdata", "fixture"), invPath, sumPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(invPath) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(inv.Unreachable))
	for _, e := range inv.Unreachable {
		got = append(got, e.Package+"."+e.Identifier+" "+e.Kind)
	}
	if want := []string{"a.T.NotCalled method"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unreached %v, want %v", got, want)
	}
	// V, ViaInit, Called, T, T.Called, I, T.NotCalled.
	if inv.Summary.TotalExported != 7 || inv.Initializers == 0 {
		t.Errorf("total_exported %d (want 7), initializers %d", inv.Summary.TotalExported, inv.Initializers)
	}
}
