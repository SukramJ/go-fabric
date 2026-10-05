// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build !unix

package main

import "errors"

// makeFIFO reports that named pipes need a Unix host.
func makeFIFO(string) error { return errors.New("--app-pipe needs a Unix host (named pipes)") }

// unblockFIFO has nothing to unblock without named pipes.
func unblockFIFO(string) {}
