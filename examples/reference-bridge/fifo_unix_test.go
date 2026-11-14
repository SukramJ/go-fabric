// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestAppPipeIsNotWorldWritable: the app pipe takes test commands, so the
// world has no access to it (W5-ex-14).
func TestAppPipeIsNotWorldWritable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "app.fifo")
	if err := makeFIFO(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != fifoMode {
		t.Errorf("app pipe mode %o, want %o", mode, fifoMode)
	}
}

// TestAppPipeExistingIsMadePrivate: a pipe already at the path — left
// world-writable by an earlier build, or planted — is not taken as it is:
// the daemon's own pipe is set to fifoMode, a symbolic link is refused, and
// another user's pipe is refused (review II, R2-P1-misc-06).
func TestAppPipeExistingIsMadePrivate(t *testing.T) {
	t.Parallel()
	t.Run("the daemon's own world-writable pipe", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "app.fifo")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
		if err := makeFIFO(path); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := st.Mode().Perm(); mode != fifoMode {
			t.Errorf("app pipe mode %o, want %o", mode, fifoMode)
		}
	})
	t.Run("a symbolic link to a pipe", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		target := filepath.Join(dir, "elsewhere.fifo")
		if err := syscall.Mkfifo(target, 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "app.fifo")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := makeFIFO(path); err == nil {
			t.Error("a symbolic link at --app-pipe was accepted")
		}
	})
	t.Run("another user's pipe", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() != 0 {
			t.Skip("handing a pipe to another user needs root")
		}
		path := filepath.Join(t.TempDir(), "app.fifo")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if err := makeFIFO(path); err == nil {
			t.Error("another user's pipe at --app-pipe was accepted")
		}
	})
}
