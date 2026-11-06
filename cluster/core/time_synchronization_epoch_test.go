// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestMatterEpochSeconds holds MatterEpochSeconds to the UTCTime rule:
// seconds since 2000-01-01 UTC, false before the epoch and past epoch-s.
func TestMatterEpochSeconds(t *testing.T) {
	for _, tc := range []struct {
		at   time.Time
		want uint32
		ok   bool
	}{
		{time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), 0, true},
		{time.Date(2000, 1, 1, 0, 1, 0, 0, time.UTC), 60, true},
		{time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC), 1700000000 - 946684800, true},
		{time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC), 0, false},
		{time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), 0, false},
	} {
		got, ok := core.MatterEpochSeconds(clocktest.New(tc.at))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v: got %d, %v; want %d, %v", tc.at, got, ok, tc.want, tc.ok)
		}
	}
	ts := core.NewTimeSynchronization()
	ts.SetClock(clocktest.New(time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)))
	if utc, ok := ts.UTC(); !ok || utc.Unix() != 946684800+86400 {
		t.Errorf("UTC: %v, %v", utc, ok)
	}
}
