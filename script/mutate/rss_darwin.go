// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"os/exec"
)

// groupRSS is the resident memory, in bytes, of every process of group
// pgid, summed from ps(1).
func groupRSS(pgid int) (int64, error) {
	out, err := exec.CommandContext(context.Background(), "ps", "-A", "-o", "pgid=,rss=").Output()
	if err != nil {
		return 0, err
	}
	return sumPsGroupRSS(out, pgid), nil
}
