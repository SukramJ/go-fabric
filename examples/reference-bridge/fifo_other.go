// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !unix

package main

import (
	"errors"
	"os"
)

// makeFIFO reports that named pipes need a Unix host.
func makeFIFO(string) error { return errors.New("--app-pipe needs a Unix host (named pipes)") }

// unblockFIFO has nothing to unblock without named pipes.
func unblockFIFO(string) {}

// openFIFOReader reports that named pipes need a Unix host.
func openFIFOReader(string) (*os.File, error) {
	return nil, errors.New("--app-pipe needs a Unix host (named pipes)")
}
