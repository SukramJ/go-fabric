// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"
	"time"
)

// TestTimeSnapshotPosixFollowsTimeSynchronization mirrors matter.js
// GeneralDiagnosticsServer.timeSnapshot: PosixTimeMs is set only when the
// node has a TimeSynchronization cluster whose UTCTime is not null — and
// TC-DGGEN-2.4 fails a node that reports one without it.
func TestTimeSnapshotPosixFollowsTimeSynchronization(t *testing.T) {
	t.Parallel()
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	snap := func() TimeSnapshotResponse {
		t.Helper()
		v, err := g.MatterInvoke(context.Background(), gendiagCmdTimeSnapshot, nil)
		if err != nil {
			t.Fatal(err)
		}
		return v.(TimeSnapshotResponse)
	}
	if r := snap(); r.PosixTimeMs != nil {
		t.Errorf("without TimeSynchronization PosixTimeMs = %d, want null", *r.PosixTimeMs)
	}
	g.SetUTCClock(func() (time.Time, bool) { return time.Time{}, false })
	if r := snap(); r.PosixTimeMs != nil {
		t.Error("with a null UTCTime PosixTimeMs is set")
	}
	ts := NewTimeSynchronization()
	g.SetUTCClock(ts.UTC)
	before := time.Now().UnixMilli()
	r := snap()
	if r.PosixTimeMs == nil || int64(*r.PosixTimeMs) < before || int64(*r.PosixTimeMs) > time.Now().UnixMilli() {
		t.Fatalf("PosixTimeMs = %v, want the UTC time now", r.PosixTimeMs)
	}
	utc, ok := ts.MatterRead(timeSyncAttrUTCTime)
	if !ok || utc == nil {
		t.Fatal("UTCTime is null on a host clock past the Matter epoch")
	}
	if got := ts.MatterAcceptedCommands(); !slices.Equal(got, []uint32{timeSyncCmdSetUTCTime}) {
		t.Errorf("AcceptedCommandList = %v, want SetUTCTime", got)
	}
	if got := ts.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("GeneratedCommandList = %v, want empty", got)
	}
}
