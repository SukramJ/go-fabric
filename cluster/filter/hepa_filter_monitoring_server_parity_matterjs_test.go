// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/hepa-filter-monitoring/HepaFilterMonitoringServerTest.ts
// at 862ad75f: "properly types state" and "rejects a condition above 100
// percent"; the construction cases are TestParityMatterJS_FilterServers.
//
// matter.js adds an AirPurifierDevice with
// HepaFilterMonitoringServer.with("Condition") and writes the condition
// inside an act; the Go form is the server built with the Condition
// feature and its host-side SetState. matter.js's refusal is a
// ConstraintError in the aggregate the node's start throws; the Go
// refusal is the construction's ErrInvalidValue (Condition is a percent).
package filter_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/filter"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

func conditionConfig(condition uint8) filter.Config {
	return filter.Config{
		Features:             filter.FeatureCondition,
		DegradationDirection: hepa.DegradationDirectionDown,
		Initial:              filter.State{Condition: condition, ChangeIndication: filter.ChangeIndicationOk},
	}
}

// TestHepaFilterMonitoringServerParityMatterJS ports the two cases of the
// describes "HepaFilterMonitoringServer" (:15-63) and
// "HepaFilterMonitoringServer condition" (:65-91).
func TestHepaFilterMonitoringServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :47-62.
	t.Run("properly types state", func(t *testing.T) {
		t.Parallel()
		srv, err := filter.NewHepaFilterMonitoring(conditionConfig(100))
		if err != nil {
			t.Fatal(err)
		}
		st := srv.State()
		st.Condition = 50
		if err := srv.SetState(st); err != nil {
			t.Fatal(err)
		}
		if got := srv.State().Condition; got != 50 {
			t.Errorf("condition %d, want 50", got)
		}
		if v, _ := srv.MatterRead(hepa.AttrCondition); v != uint8(50) {
			t.Errorf("Condition reads %v, want 50", v)
		}
	})

	// :83-90.
	t.Run("rejects a condition above 100 percent", func(t *testing.T) {
		t.Parallel()
		if _, err := filter.NewHepaFilterMonitoring(conditionConfig(101)); !errors.Is(err, filter.ErrInvalidValue) {
			t.Fatalf("condition 101: %v, want ErrInvalidValue", err)
		}
	})
}
