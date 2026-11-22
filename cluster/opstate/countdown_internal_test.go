// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate

import (
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/contract"
)

// TestCountdownTimeIsReportedAsAQuieterAttribute: the value is read back
// at once and its change advances the DataVersion at once, but its change
// report follows matter.js's QuietEvent — at once from or to null, else
// at most once a second with the latest value — and the deferred report
// advances nothing more. CountdownTime is ordinary state in matter.js: the
// commit that changed it advances the version
// (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
// db021da0), and the quiet emit adds an advance only for a dynamic
// property (ServerBehaviorBacking.ts:258-261 calls advanceVersionFor,
// Datasource.ts:679-688), which OperationalStateServer declares none of.
func TestCountdownTimeIsReportedAsAQuieterAttribute(t *testing.T) {
	t.Parallel()
	srv, err := NewServer(Config{States: []StateEntry{{ID: StateStopped}, {ID: StateError}}, CountdownTime: true})
	if err != nil {
		t.Fatal(err)
	}
	clk := clocktest.New(time.Unix(5000, 0))
	srv.quiet.Clock = clk
	var reports [][]uint32
	srv.OnAttributeReport(func(change contract.Change) { ids := change.Attributes; reports = append(reports, ids) })
	set := func(v *uint32) {
		t.Helper()
		if err := srv.SetCountdownTime(v); err != nil {
			t.Fatal(err)
		}
	}
	u := func(v uint32) *uint32 { return &v }

	set(u(600)) // null → 600: at once
	if len(reports) != 1 || !slices.Equal(reports[0], []uint32{AttrCountdownTime}) {
		t.Fatalf("reports %v, want CountdownTime once", reports)
	}
	version := srv.MatterDataVersion()
	set(u(599)) // within the second: held
	set(u(599)) // unchanged: nothing
	if v, _ := srv.MatterRead(AttrCountdownTime); v != uint32(599) {
		t.Errorf("CountdownTime read %v, want 599 at once", v)
	}
	if got := srv.MatterDataVersion(); got != version+1 {
		t.Fatalf("the held change moved the DataVersion %d -> %d, want +1 at once", version, got)
	}
	if len(reports) != 1 || clk.Pending() != 1 {
		t.Fatalf("a held change reported: %v (timers %d)", reports, clk.Pending())
	}
	clk.Advance(time.Second)
	if len(reports) != 2 {
		t.Fatalf("the deferred report did not go out: %v", reports)
	}
	if got := srv.MatterDataVersion(); got != version+1 {
		t.Fatalf("the deferred report moved the DataVersion %d -> %d, want no further advance", version+1, got)
	}
	set(nil) // → null: at once
	if len(reports) != 3 {
		t.Fatalf("to null: %v", reports)
	}
}
