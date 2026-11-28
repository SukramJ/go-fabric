// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux || darwin

package main

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// watchSupported: the group RSS can be read and the group killed here.
const watchSupported = true

// ownGroup starts cmd as the leader of a new process group, whose id is
// then its pid; the go tool's children — the compiler, the linker and the
// test binary — join it. A cancelled context kills the whole group, not
// only cmd's own pid.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killGroup(cmd.Process.Pid)
		return nil
	}
}

// killGroup sends SIGKILL to every process of group pgid and waits, at
// most 10 s, until no process of the group exists any more: the test
// binary, orphaned when the go tool dies, is reaped by init, and the next
// mutant should not start beside its memory.
func killGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for range 1000 {
		if errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) {
			return
		}
		<-t.C
	}
}
