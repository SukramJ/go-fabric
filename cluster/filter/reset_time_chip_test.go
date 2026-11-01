// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/filter"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

// TestResetConditionStampsLastChangedTime: ResetCondition sets a served
// LastChangedTime to the time of the reset, in seconds since the Matter
// epoch, as connectedhomeip's OnResetCondition does
// (ResourceMonitoringCluster.cpp:375-385 at 6170af84; W3-club2-03); a
// server without the attribute keeps none.
func TestResetConditionStampsLastChangedTime(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	srv, err := filter.NewHepaFilterMonitoring(filter.Config{
		Features: filter.FeatureCondition, Optional: filter.OptionalLastChangedTime | filter.OptionalResetCondition,
		Resetter: &host{}, Clock: clk, Initial: filter.State{LastChangedTime: u32(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	want := uint32(clk.Now().Unix() - 946684800)
	if v, _ := srv.MatterRead(hepa.AttrLastChangedTime); v != want {
		t.Errorf("LastChangedTime %v after the reset, want %d", v, want)
	}
	if st := srv.State(); st.LastChangedTime == nil || *st.LastChangedTime != want {
		t.Errorf("State().LastChangedTime %v, want %d", st.LastChangedTime, want)
	}
}
