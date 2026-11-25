// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux || darwin

package main

import "syscall"

// freeGiB is the free space of the volume holding dir, in GiB; a large
// number when it cannot be read, so the cache is never cleaned on a guess.
func freeGiB(dir string) int {
	var st syscall.Statfs_t
	if dir == "" || syscall.Statfs(dir, &st) != nil {
		return 1 << 20
	}
	return int(st.Bavail * uint64(st.Bsize) >> 30) //nolint:gosec // block counts and sizes are non-negative
}
