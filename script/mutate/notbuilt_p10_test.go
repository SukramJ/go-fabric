// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMutateNotBuiltP10 (P10-test-6903): every mutant of a file the
// build excludes — here by `//go:build ignore` — is NOTBUILT, not
// SURVIVED: the overlay replaces a file no test compiles.
func TestMutateNotBuiltP10(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := writeFixture(t)
	ign := "//go:build ignore\n\npackage p\n\nfunc neg(x int) bool { return x < 0 }\n"
	if err := os.WriteFile(filepath.Join(root, "p", "ign.go"), []byte(ign), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "mutants.tsv")
	var stderr bytes.Buffer
	if code := run([]string{"-root", root, "-p", "1", "-timeout", "60s", "-o", out, "p/ign.go"}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "NOTBUILT\t\t\tp/ign.go:5\top <→<=\tfunc neg(x int) bool { return x < 0 }\n"; string(got) != want {
		t.Errorf("TSV:\n%s\nwant:\n%s", got, want)
	}
}

// TestMutateOtherGOOSFileNotBuiltP10 (P10-test-6903, the probe
// rss_linux_on_darwin.tsv): this package's file for the other system is
// outside the build here, so both its mutants are NOTBUILT — on the
// runner of the probe they read SURVIVED.
func TestMutateOtherGOOSFileNotBuiltP10(t *testing.T) {
	file := map[string]string{"darwin": "script/mutate/rss_linux.go", "linux": "script/mutate/rss_darwin.go"}[runtime.GOOS]
	if file == "" {
		t.Skipf("no file of this package is excluded on %s alone", runtime.GOOS)
	}
	t.Setenv("GOFLAGS", "")
	out := filepath.Join(t.TempDir(), "mutants.tsv")
	var stderr bytes.Buffer
	if code := run([]string{"-root", filepath.Join("..", ".."), "-p", "1", "-o", out, file}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(got, []byte("NOTBUILT\t")); n == 0 || n != bytes.Count(got, []byte("\n")) {
		t.Errorf("TSV:\n%s\nwant every line NOTBUILT", got)
	}
}
