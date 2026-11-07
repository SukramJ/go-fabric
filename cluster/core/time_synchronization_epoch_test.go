// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
)

// TestMatterEpochSeconds holds the server's MatterEpochSeconds to the
// UTCTime rule: seconds since 2000-01-01 UTC, false before the epoch (no
// time) and past epoch-s; SetUTCTime moves it with UTC, the one Matter
// time of the node (connectedhomeip's GetClock_MatterEpochS over the
// clock SetUTCTime sets, SystemClock.cpp:284-306 and
// TimeSynchronizationCluster.cpp:856-872 at 6170af84).
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
		ts := core.NewTimeSynchronization()
		ts.SetClock(clocktest.New(tc.at))
		got, ok := ts.MatterEpochSeconds()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v: got %d, %v; want %d, %v", tc.at, got, ok, tc.want, tc.ok)
		}
	}
	ts := core.NewTimeSynchronization()
	ts.SetClock(clocktest.New(time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)))
	if utc, ok := ts.UTC(); !ok || utc.Unix() != 946684800+86400 {
		t.Errorf("UTC: %v, %v", utc, ok)
	}
	// SetUTCTime a day later: the epoch seconds follow the offset.
	at := time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC)
	if _, err := ts.MatterInvoke(context.Background(), tsdef.CmdSetUtcTime, tsdef.SetUtcTimeRequest{
		UtcTime: uint64(at.UnixMicro() - 946684800_000_000), Granularity: tsdef.GranularityMillisecondsGranularity, //nolint:gosec // after the epoch
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := ts.MatterEpochSeconds(); !ok || got != 2*86400 {
		t.Errorf("after SetUTCTime: got %d, %v; want %d", got, ok, 2*86400)
	}
}
