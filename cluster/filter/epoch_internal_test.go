// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter

import (
	"testing"
	"time"
)

// TestMatterEpochSeconds: seconds since 2000-01-01 UTC; a time before the
// epoch or beyond the non-null uint32 range has none (W3-club2-03).
func TestMatterEpochSeconds(t *testing.T) {
	t.Parallel()
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Time
		want uint32
		ok   bool
	}{
		{epoch, 0, true},
		{epoch.Add(90 * time.Second), 90, true},
		{epoch.Add(-time.Second), 0, false},
		{epoch.Add((1<<32 - 1) * time.Second), 0, false},
	} {
		if got, ok := matterEpochSeconds(c.at); got != c.want || ok != c.ok {
			t.Errorf("matterEpochSeconds(%v) = %d, %v; want %d, %v", c.at, got, ok, c.want, c.ok)
		}
	}
}
