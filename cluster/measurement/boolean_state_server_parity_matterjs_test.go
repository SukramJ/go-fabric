// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/boolean-state/BooleanStateServerTest.ts
// at 862ad75f, "enables the ChangeEvent feature by default"; the emission
// is bridge:TestBooleanStateChangeEvent.
//
// matter.js adds a ContactSensorDevice and finds the StateChange event on
// its BooleanStateServer, which exists only with the ChangeEvent feature;
// the Go server built for a contact sensor's reading advertises the
// feature in its FeatureMap and lists StateChange in its EventList.
package measurement_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	booldef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	"github.com/SukramJ/go-fabric/contract"
)

// TestBooleanStateServerParityMatterJS ports :12-17.
func TestBooleanStateServerParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("enables the ChangeEvent feature by default", func(t *testing.T) {
		t.Parallel()
		s := measurement.NewBooleanStateServer(fakeBool{class: contract.MeasurementContact, val: false, obs: true})
		fm, ok := s.MatterRead(cluster.AttrGlobalFeatureMap)
		if v, isU32 := fm.(uint32); !ok || !isU32 || v&uint32(booldef.FeatureChangeEvent) == 0 {
			t.Errorf("FeatureMap %v, want ChangeEvent set", fm)
		}
		if !slices.Contains(s.MatterEvents(), booldef.EventStateChange) {
			t.Errorf("EventList %v lacks StateChange", s.MatterEvents())
		}
	})
}
