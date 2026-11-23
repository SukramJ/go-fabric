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
// package-level var's initializer reads as reached (R2-P6-test-2805), an
// exported method no test calls is listed as unreached (R2-P6-test-2806),
// one an unexported type declares included, on the exported type it is
// promoted into (R2-C2-test-2803), and an annotated identifier a test
// reaches is listed as a redundant exemption (R2-C2-test-2802).
func TestAnalyzerSeedsInitializersAndListsMethods(t *testing.T) {
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
	if want := []string{"a.Outer.Promoted method", "a.T.NotCalled method"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unreached %v, want %v", got, want)
	}
	// V, ViaInit, Called, T, T.Called, I, T.NotCalled, Outer,
	// Outer.Promoted, Annotated.
	if inv.Summary.TotalExported != 10 || inv.Initializers == 0 {
		t.Errorf("total_exported %d (want 10), initializers %d", inv.Summary.TotalExported, inv.Initializers)
	}
	redundant := make([]string, 0, len(inv.RedundantAnnotations))
	for _, e := range inv.RedundantAnnotations {
		redundant = append(redundant, e.Package+"."+e.Identifier)
	}
	if want := []string{"a.Annotated"}; !reflect.DeepEqual(redundant, want) {
		t.Errorf("redundant annotations %v, want %v", redundant, want)
	}
}
