// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStatGroupPagesSkipsWhatItCannotRead: only the group's processes are
// summed; a stat file that is gone (its process exited after the listing)
// or does not parse is skipped. The skip used to be reached only when a
// process happened to exit during a watchdog poll, so the package's
// coverage moved between runs.
func TestStatGroupPagesSkipsWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Fields from 3 on: state ppid pgrp … rss is field 24.
	stat := func(pgrp, rss string) string {
		return "42 (go test) S 1 " + pgrp + " 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 " + rss + " 0"
	}
	paths := []string{
		write("a", stat("7", "100")),
		filepath.Join(dir, "gone"),
		write("b", stat("8", "1000")),
		write("c", "not a stat file"),
		write("d", stat("7", "23")),
	}
	if got := statGroupPages(paths, 7); got != 123 {
		t.Fatalf("statGroupPages(group 7) = %d, want 123", got)
	}
	if got := statGroupPages(paths, 9); got != 0 {
		t.Fatalf("statGroupPages(group 9) = %d, want 0", got)
	}
}
