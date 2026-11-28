// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux || darwin

package main

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestOwnGroupCancelKillsTheGroupP10 (P10-conc-6901): a command started
// through ownGroup and cancelled by its context takes its whole process
// group down, not only its own pid — a child it spawned is gone once Wait
// returns, as the go tool's compiler or test binary must be when goTest's
// deadline fires.
func TestOwnGroupCancelKillsTheGroupP10(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60 >/dev/null 2>&1 & echo $!; wait")
	ownGroup(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	cancel()
	_ = cmd.Wait()
	if err := syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("child %d of the group still exists after the cancelled Wait (kill 0: %v)", child, err)
	}
}
