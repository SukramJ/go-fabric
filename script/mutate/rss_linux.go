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
	return statGroupPages(stats, pgid) * int64(os.Getpagesize()), nil
}
