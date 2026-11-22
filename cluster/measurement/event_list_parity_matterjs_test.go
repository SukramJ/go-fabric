// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/measurement"
	"thingfabric.dev/go-fabric/contract"
)

// TestPowerServersListNoEvent (review II, R2-P4-nw-05): PowerSource and
// ElectricalPowerMeasurement have events in the model, and neither server
// emits one, so each lists none — an event path on them selects nothing.
// matter.js gives a server's cluster type an event only when the selection
// makes it mandatory or the behaviour implements it
// (packages/node/src/node/integration/ProtocolService.ts:529-532,
// packages/node/src/behavior/internal/ClusterDerivation.ts:196-200 at
// db021da0). PowerSource's three events are optional under every feature
// ("[WIRED]", "[BAT]", "[RECHG]",
// packages/model/src/standard/elements/power-source-cluster.element.ts:135,
// :147, :159) and PowerSourceServer emits none
// (packages/node/src/behaviors/power-source/PowerSourceServer.ts:21-63);
// MeasurementPeriodRanges is mandatory with the optional Ranges attribute
// (electrical-power-measurement.element.ts:124, :47), which the server does
// not serve, and ElectricalPowerMeasurementServer emits nothing
// (packages/node/src/behaviors/electrical-power-measurement/ElectricalPowerMeasurementServer.ts:18).
func TestPowerServersListNoEvent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		srv  contract.ClusterServer
	}{
		{"PowerSource, battery level", measurement.NewPowerSourceServer(fakeBool{val: false, obs: true})},
		{"PowerSource, battery percentage", measurement.NewPowerSourceServerFromFloat(fakeFloat{val: 50, obs: true})},
		{"PowerSource, wired", measurement.NewWiredPowerSourceServer(contract.WiredCurrentAC)},
		{"ElectricalPowerMeasurement", measurement.NewElectricalPowerServer(fakeFloat{class: contract.MeasurementPower, val: 1500, obs: true})},
	} {
		lister, ok := tc.srv.(contract.ClusterEventLister)
		if !ok {
			t.Errorf("%s: the server does not list its events", tc.name)
			continue
		}
		if got := lister.MatterEvents(); len(got) != 0 {
			t.Errorf("%s: lists events %v, want none — the server emits none", tc.name, got)
		}
	}
}
