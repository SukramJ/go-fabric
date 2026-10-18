// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestCountdownTimeIsReportedAsAQuieterAttribute: the value is read back
// at once, but its change report follows matter.js's QuietEvent — at once
// from or to null, else at most once a second with the latest value — and
// a held change advances neither the DataVersion nor anything a
// subscriber sees.
func TestCountdownTimeIsReportedAsAQuieterAttribute(t *testing.T) {
	t.Parallel()
	srv, err := NewServer(Config{States: []StateEntry{{ID: StateStopped}, {ID: StateError}}, CountdownTime: true})
	if err != nil {
		t.Fatal(err)
	}
	clk := clocktest.New(time.Unix(5000, 0))
	srv.quiet.Clock = clk
	var reports [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { reports = append(reports, ids) })
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
	if len(reports) != 1 || srv.MatterDataVersion() != version || clk.Pending() != 1 {
		t.Fatalf("a held change reported: %v (version moved %v, timers %d)", reports, srv.MatterDataVersion() != version, clk.Pending())
	}
	clk.Advance(time.Second)
	if len(reports) != 2 || srv.MatterDataVersion() == version {
		t.Fatalf("the deferred report did not go out: %v", reports)
	}
	set(nil) // → null: at once
	if len(reports) != 3 {
		t.Fatalf("to null: %v", reports)
	}
}
