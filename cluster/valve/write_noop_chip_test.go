// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/valve"
)

// TestDefaultOpenDurationUnchangedIsNoOp: a write of the current
// DefaultOpenDuration — a value or null — succeeds without reaching the
// host and without moving the DataVersion, as connectedhomeip answers it
// kWriteSuccessNoOp (ValveConfigurationAndControlCluster.cpp:165-166 at
// 6170af84; W3-club2-05); a changed value still reaches the host.
func TestDefaultOpenDurationUnchangedIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})
	version := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(300)); err != nil {
		t.Fatal(err)
	}
	if len(host.defaultSets) != 0 || srv.MatterDataVersion() != version {
		t.Fatalf("an unchanged write reached the host (%d) or moved the DataVersion", len(host.defaultSets))
	}
	host.defaultKnown = false
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, nil); err != nil {
		t.Fatal(err)
	}
	if len(host.defaultSets) != 0 || srv.MatterDataVersion() != version {
		t.Fatal("an unchanged null write reached the host or moved the DataVersion")
	}
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(60)); err != nil {
		t.Fatal(err)
	}
	if len(host.defaultSets) != 1 || srv.MatterDataVersion() == version {
		t.Fatal("a changed write did not reach the host or move the DataVersion")
	}
}
