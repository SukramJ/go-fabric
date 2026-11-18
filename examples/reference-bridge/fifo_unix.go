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

// makeFIFO creates the named pipe at path unless one is already there. A
// pipe already there is taken only when it is the daemon's own: a symbolic
// link or another user's pipe is refused, and the daemon's own pipe is
// set to fifoMode, so one an earlier build left world-writable is not
// (review II, R2-P1-misc-06). Every check and the mode change act on one
// open descriptor (O_NOFOLLOW, fstat, fchmod), never on the path again: in
// a directory another user can write, the path cannot be swapped between
// the check and the chmod (R2-P6-sec-2614).
func makeFIFO(path string) error {
	if err := syscall.Mkfifo(path, fifoMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	// O_NONBLOCK: a FIFO's read end opens without waiting for a writer.
	fd, st, err := openOwnFIFO(path, syscall.O_RDONLY|syscall.O_NONBLOCK)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Close(fd) }()
	// mkfifo honours the umask, and an earlier build left the mode
	// wider: the mode is set as stated, on the descriptor.
	if fs.FileMode(st.Mode)&fs.ModePerm == fifoMode {
		return nil
	}
	if err := syscall.Fchmod(fd, fifoMode); err != nil {
		return fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	return nil
}

// openFIFOReader opens the app pipe's read end for one writer, waiting for
// it, and reads from the descriptor it checked: the daemon's own named
// pipe, reached without following a symbolic link (R2-P6-sec-2614).
func openFIFOReader(path string) (*os.File, error) {
	fd, _, err := openOwnFIFO(path, syscall.O_RDONLY)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// openOwnFIFO opens path with flags, refusing a symbolic link
// (O_NOFOLLOW), and keeps the descriptor only when fstat shows the
// daemon's own named pipe.
func openOwnFIFO(path string, flags int) (int, syscall.Stat_t, error) {
	var st syscall.Stat_t
	var fd int
	var err error
	for {
		fd, err = syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if errors.Is(err, syscall.ELOOP) {
		return -1, st, fmt.Errorf("--app-pipe %s is a symbolic link, not the daemon's own named pipe", path)
	}
	if err != nil {
		return -1, st, fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = syscall.Close(fd)
		return -1, st, fmt.Errorf("--app-pipe %s: %w", path, err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		_ = syscall.Close(fd)
		return -1, st, fmt.Errorf("--app-pipe %s exists and is not a named pipe", path)
	}
	if int(st.Uid) != os.Geteuid() {
		_ = syscall.Close(fd)
		return -1, st, fmt.Errorf("--app-pipe %s is not the daemon's own named pipe", path)
	}
	return fd, st, nil
}

// unblockFIFO opens and closes the write end once, so a reader blocked in
// open(2) returns.
func unblockFIFO(path string) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = syscall.Close(fd)
	}
}
