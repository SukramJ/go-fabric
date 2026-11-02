// SPDX-License-Identifier: Apache-2.0
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

// fifoMode is the app pipe's mode: owner and group, never the world — a
// local user must not inject test commands (W5-ex-14). The writer is not
// always the daemon's user: the chip-tool harness's Python cases write it
// from the CHIP image's container, which runs as root
// (internal/chiptool/harness_test.go, docker run without --user) and
// needs no mode bit; a harness run under another account of the daemon's
// group needs the group's write bit.
const fifoMode = 0o660

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
	if err := syscall.Mkfifo(path, fifoMode); err != nil {
		return fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	// mkfifo honours the umask; the mode is set as stated.
	return os.Chmod(path, fifoMode)
}

// unblockFIFO opens and closes the write end once, so a reader blocked in
// open(2) returns.
func unblockFIFO(path string) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = syscall.Close(fd)
	}
}
