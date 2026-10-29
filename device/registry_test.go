// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestLookup: the registry finds a generated type by its 32-bit device
// type id and misses an unknown id and one above 0xFFFF.
func TestLookup(t *testing.T) {
	t.Parallel()
	if ty, ok := Lookup(uint32(OnOffPlugInUnitID)); !ok || ty != &OnOffPlugInUnitType || ty.Name != "OnOffPlugInUnit" {
		t.Fatalf("Lookup(OnOffPlugInUnit) %v %v", ty, ok)
	}
	for _, id := range []uint32{
		0xFFF1_0001, // manufacturer-specific
		0x0001_010A, // above 0xFFFF: not OnOffPlugInUnit
		0xFFFF_FFFF, // the client view's unknown device type
	} {
		if ty, ok := Lookup(id); ok || ty != nil {
			t.Errorf("Lookup(%#x) = %v", id, ty.Name)
		}
	}
	for _, ty := range Types() {
		if got, ok := Lookup(uint32(ty.ID)); !ok || got != ty {
			t.Errorf("Lookup(%#x) misses %s", ty.ID, ty.Name)
		}
	}
}

// TestEveryGeneratedFileIsRegistered: each device/<name>_gen.go that
// declares a device type is in the registry Lookup reads, and the registry
// holds nothing else — as cluster/spec's generated packages register.
func TestEveryGeneratedFileIsRegistered(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^var type(\w+) = Type\{`)
	var declared []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			declared = append(declared, m[1])
		}
	}
	registered := make([]string, 0, len(committedTypes))
	for _, ty := range committedTypes {
		registered = append(registered, ty.Name)
	}
	slices.Sort(declared)
	slices.Sort(registered)
	if len(declared) == 0 || !slices.Equal(declared, registered) {
		t.Fatalf("declared %v, registered %v", declared, registered)
	}
}
