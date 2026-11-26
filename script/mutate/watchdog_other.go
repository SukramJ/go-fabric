// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !linux && !darwin

package main

import (
	"errors"
	"os/exec"
)

// watchSupported: neither the group RSS nor the group kill exists here, so
// -maxrss has no effect.
const watchSupported = false

func ownGroup(*exec.Cmd) {}

func killGroup(int) {}

func groupRSS(int) (int64, error) { return 0, errors.ErrUnsupported }
