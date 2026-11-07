// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestMatterTimeOf holds MatterTimeOf to the epoch-s rule: seconds since
// 2000-01-01 UTC, false before the epoch and past 32 bits; it follows
// the clock it reads.
func TestMatterTimeOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		at   time.Time
		want uint32
		ok   bool
	}{
		{time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), 0, true},
		{time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC), 1700000000 - 946684800, true},
		{time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC), 0, false},
		{time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), 0, false},
	} {
		got, ok := clock.MatterTimeOf(clocktest.New(tc.at))()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v: got %d, %v; want %d, %v", tc.at, got, ok, tc.want, tc.ok)
		}
	}
	fake := clocktest.New(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	mt := clock.MatterTimeOf(fake)
	fake.Advance(90 * time.Second)
	if got, ok := mt(); !ok || got != 90 {
		t.Errorf("after 90 s: %d, %v", got, ok)
	}
	if _, ok := clock.MatterTimeOf(nil)(); !ok {
		t.Error("the system clock reads no Matter time")
	}
}
