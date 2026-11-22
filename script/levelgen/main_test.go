// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/script/levelgen/leveltable"
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

// TestRunRestoresTheTableWhenTheDocFails: levels_gen.go is written before
// the reference daemon is built (the build needs it); when the doc's
// measurement then fails, the run puts the file back as it was, so a
// failed run leaves no table beside a stale doc (R2-P6-err-2709). The
// tree is a copy of the module's inputs with a stale levels_gen.go; GO
// names a tool that fails, so the daemon build fails.
func TestRunRestoresTheTableWhenTheDocFails(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	copyFile := func(rel string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(src, rel)) //nolint:gosec // G304: the module's own files
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	levels, err := filepath.Glob(filepath.Join(src, levelsDir, "*.json"))
	if err != nil || len(levels) == 0 {
		t.Fatalf("level files: %v %v", levels, err)
	}
	for _, l := range levels {
		copyFile(filepath.Join(levelsDir, filepath.Base(l)))
	}
	copyFile(pinPath)
	copyFile(docPath)
	const stale = "// stale\n"
	if err := os.MkdirAll(filepath.Join(root, "schema"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, goPath), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO", "false")
	t.Chdir(root)
	if err := run(true); err == nil || !strings.Contains(err.Error(), "build the reference daemon") {
		t.Fatalf("run = %v, want the daemon build's failure", err)
	}
	if b, _ := os.ReadFile(goPath); string(b) != stale {
		t.Errorf("levels_gen.go after a failed run:\n%.200s\nwant the stale file back", b)
	}
}

// TestMeasureInternalHasADeadline: a daemon build (or dump) that hangs
// fails the run after its deadline, naming it, instead of hanging
// make generate-level(s) without a message (R2-P6-err-2717). GO names a
// stand-in that sleeps.
func TestMeasureInternalHasADeadline(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { //nolint:gosec // G306: an executable stand-in
		t.Fatal(err)
	}
	t.Setenv("GO", hang)
	defer func(d time.Duration) { buildTimeout = d }(buildTimeout)
	buildTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := measureInternal([]leveltable.File{{}})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("measureInternal = %v, want the build's deadline", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the hanging build returned after %v", d)
	}
}
