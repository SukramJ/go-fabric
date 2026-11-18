// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// TestMentioningFilesReadsTrackedFilesOnly: a copy of the module in an
// untracked worktree under .claude/ (the state of a checkout with agent
// worktrees) or an untracked file is no caller; a failing git grep is an
// error, not "no mention" (R2-P6-test-2807).
func TestMentioningFilesReadsTrackedFilesOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a/decl.go", "package a\n\nfunc Target() {}\n")
	write("b/use.go", "package b\n\nvar _ = Target\n")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	write(".claude/worktrees/x/c.go", "package x\n\nvar _ = Target\nvar _ = Lonely\n")
	write("d/untracked.go", "package d\n\nvar _ = Lonely\n")

	got, err := mentioningFiles(root, "Target", "a/decl.go")
	if err != nil || !reflect.DeepEqual(got, []string{filepath.Join("b", "use.go")}) {
		t.Errorf("Target: %v, %v; want [b/use.go]", got, err)
	}
	if got, err := mentioningFiles(root, "Lonely", "a/decl.go"); err != nil || len(got) != 0 {
		t.Errorf("Lonely, named only by untracked copies: %v, %v; want none", got, err)
	}
	if _, err := mentioningFiles(t.TempDir(), "Target", "a/decl.go"); err == nil {
		t.Error("git grep outside a repository read as no mention")
	}
	for name, want := range map[string]bool{".claude": true, ".git": true, "vendor": true, "a": false, ".": false} {
		if skipWalkDir(name) != want {
			t.Errorf("skipWalkDir(%q) = %v", name, !want)
		}
	}
}
