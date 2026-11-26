// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"os"
	"path/filepath"
)

// groupRSS is the resident memory, in bytes, of every process of group
// pgid, summed from /proc/<pid>/stat. A process that exits between the
// listing and the read is skipped.
func groupRSS(pgid int) (int64, error) {
	stats, err := filepath.Glob("/proc/[0-9]*/stat")
	if err != nil {
		return 0, err
	}
	var pages int64
	for _, p := range stats {
		b, err := os.ReadFile(p) //nolint:gosec // a /proc stat file
		if err != nil {
			continue
		}
		if g, r, ok := parseProcStat(b); ok && g == pgid {
			pages += r
		}
	}
	return pages * int64(os.Getpagesize()), nil
}
