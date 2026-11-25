// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build !linux && !darwin

package main

// freeGiB cannot read the free space here: a large number, so -minfree
// never cleans the cache on a guess.
func freeGiB(string) int { return 1 << 20 }
