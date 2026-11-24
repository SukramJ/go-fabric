// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"reflect"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
)

// TestKeyStateFieldsAreClassified is the standing guard of R5-conc-5107: a
// write of key-held state advances the version of the instance that serves
// the key only for the clusters [holdsCluster] names. Every keyState field
// is classified here — the cluster whose state it holds, or none — so a
// field added for a further cluster fails until holdsCluster names it.
func TestKeyStateFieldsAreClassified(t *testing.T) {
	t.Parallel()
	const none = ^uint32(0)
	fields := map[string]uint32{
		"mu":              none,
		"identify":        identifyClusterID,
		"clock":           none,
		"nodeLabel":       mattercore.BridgedDeviceBasicInformationClusterID,
		"hostName":        mattercore.BridgedDeviceBasicInformationClusterID,
		"configVersion":   mattercore.BridgedDeviceBasicInformationClusterID,
		"scenes":          mattercore.ScenesManagementClusterID,
		"binding":         bindingClusterID,
		"nilServerLogged": none,
		"labelMu":         none,
		"served":          none,
	}
	typ := reflect.TypeFor[keyState]()
	seen := map[string]bool{}
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		seen[name] = true
		cluster, ok := fields[name]
		if !ok {
			t.Errorf("keyState.%s is not classified: name the cluster whose state it holds (and add it to holdsCluster) or none", name)
			continue
		}
		if cluster != none && !holdsCluster(cluster) {
			t.Errorf("keyState.%s holds cluster 0x%04X, which holdsCluster does not name", name, cluster)
		}
	}
	for name := range fields {
		if !seen[name] {
			t.Errorf("classified field keyState.%s no longer exists", name)
		}
	}
	if holdsCluster(0x0404) {
		t.Error("holdsCluster names FlowMeasurement, whose state is the instance's")
	}
}
