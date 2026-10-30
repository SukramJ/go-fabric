// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseInternal(t *testing.T) {
	b := []byte(`{"decisions":[
{"cluster":"0x0000001f","endpoint":"","id":"0x00000007","kind":"attribute","policy":"level","reason":"kept: server-internal, not level-controlled","verdict":"keep"},
{"cluster":"0x00000006","endpoint":"a","id":"0x00000001","kind":"feature","policy":"level","reason":"absent at level 1.4","verdict":"remove"},
{"cluster":"0x00000006","endpoint":"a","id":"0x00000001","kind":"feature","policy":"","reason":"","verdict":"keep"}]}`)
	rows, err := ParseInternal(b)
	if err != nil || len(rows) != 1 || rows[0].Cluster != 0x1F || rows[0].ID != 7 || rows[0].Kind != "attribute" {
		t.Fatalf("%+v, %v", rows, err)
	}
	if _, err := ParseInternal([]byte("{")); err == nil {
		t.Error("bad JSON")
	}
	bad := []byte(`{"decisions":[{"cluster":"zz","policy":"level","reason":"kept: server-internal, not level-controlled","verdict":"keep","id":"0x1"}]}`)
	if _, err := ParseInternal(bad); err == nil {
		t.Error("bad cluster id")
	}
	bad = []byte(`{"decisions":[{"cluster":"0x1","policy":"level","reason":"kept: server-internal, not level-controlled","verdict":"keep","id":"zz"}]}`)
	if _, err := ParseInternal(bad); err == nil {
		t.Error("bad id")
	}
}

func TestWriteIfChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	for range 2 {
		if err := writeIfChanged(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("%q", b)
	}
}

// TestRunWithoutDoc runs the generator over the module's own files from
// the module root: levels_gen.go is current, so nothing changes.
func TestRunWithoutDoc(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	if err := run(false); err != nil {
		t.Fatal(err)
	}
}
