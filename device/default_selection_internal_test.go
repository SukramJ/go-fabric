// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	accesscontroldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	booleanstatedef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	generaldiagnosticsdef "thingfabric.dev/go-fabric/cluster/spec/generaldiagnostics"
	groupcastdef "thingfabric.dev/go-fabric/cluster/spec/groupcast"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	icdmanagementdef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	scenesmanagementdef "thingfabric.dev/go-fabric/cluster/spec/scenesmanagement"
	timeformatlocalizationdef "thingfabric.dev/go-fabric/cluster/spec/timeformatlocalization"
	unitlocalizationdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
)

// intentionallyEnabled are the clusters of matter.js's INTENTIONALLY_ENABLED
// table (packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts:
// 27-37 at db021da0); node's TestIntentionallyEnabledFeaturesParityMatterJS
// holds each row's features.
var intentionallyEnabled = map[uint32]bool{
	accesscontroldef.ClusterID: true, booleanstatedef.ClusterID: true, generaldiagnosticsdef.ClusterID: true,
	groupcastdef.ClusterID: true, groupsdef.ClusterID: true, icdmanagementdef.ClusterID: true,
	scenesmanagementdef.ClusterID: true, timeformatlocalizationdef.ClusterID: true, unitlocalizationdef.ClusterID: true,
}

// TestBareDevicesServeTheDefaultSelection (R2-P5-apidev-12, the second half
// of ClusterBehaviorFeaturesTest.ts:201-232): every generated type built
// with its defaults serves, on each kit server its type lists, exactly the
// default selection — the type's mandated features, the cluster's "M" ones
// and the features a default server enables of its own — and a cluster
// whose default server enables features of its own is one of the table's.
func TestBareDevicesServeTheDefaultSelection(t *testing.T) {
	t.Parallel()
	for id := range serverEnabledFeatures {
		if !intentionallyEnabled[id] {
			t.Errorf("cluster 0x%04X enables features of its own and is not in INTENTIONALLY_ENABLED", id)
		}
	}
	for _, ty := range Types() {
		d, err := New(ty, testOptions(ty.Name))
		if err != nil {
			continue // refused bare: TestEveryTypeConstructsAndConforms lists why
		}
		for _, srv := range d.Servers() {
			id := srv.MatterClusterID()
			r, ok := ty.Server(id)
			if !ok || r.Definition == nil || d.hosted[id] {
				continue
			}
			v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
			fm, isUint32 := v.(uint32)
			if !ok || !isUint32 {
				t.Errorf("%s: cluster 0x%04X answers FeatureMap %v (%v)", ty.Name, id, v, ok)
				continue
			}
			if want := defaultSelection(&r); fm != want {
				t.Errorf("%s: %s FeatureMap 0x%X, the default selection is 0x%X", ty.Name, r.Name, fm, want)
			}
		}
	}
}
