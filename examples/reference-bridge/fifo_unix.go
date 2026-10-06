// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build unix

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// makeFIFO creates the named pipe at path unless one is already there.
func makeFIFO(path string) error {
	if st, err := os.Stat(path); err == nil {
		if st.Mode()&fs.ModeNamedPipe == 0 {
			return fmt.Errorf("--app-pipe %s exists and is not a named pipe", path)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := syscall.Mkfifo(path, 0o666); err != nil {
		return fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	// The pipe is written by a test harness that may run as another user
	// (a container's root); mkfifo honours the umask, so widen it here.
	return os.Chmod(path, 0o666) //nolint:gosec // a test-only control pipe, off by default
}

// unblockFIFO opens and closes the write end once, so a reader blocked in
// open(2) returns.
func unblockFIFO(path string) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = syscall.Close(fd)
	}
}
