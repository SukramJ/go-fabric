// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"fmt"
	"testing"
	"time"

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
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// TestIntentionallyEnabledFeaturesParityMatterJS ports the
// INTENTIONALLY_ENABLED table of matter.js
// packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts:27-37
// at db021da0 and the walk that holds every published behaviour to it
// (:201-232): the nine default servers that enable features of their own
// enable exactly those (none of the features is mandatory by conformance).
// Each row is read off the FeatureMap of the server the node and the
// device kit build by default — root, a light's Groups and
// ScenesManagement, a contact sensor's BooleanState, an ICD's ICDManagement
// (review II, F-db021da0-23, R2-P5-apidev-12).
func TestIntentionallyEnabledFeaturesParityMatterJS(t *testing.T) {
	t.Parallel()
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("features:light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	contact, err := device.NewContactSensor(device.Options{StableKey: endpoint.StringKey("features:contact"), Name: "contact"})
	if err != nil {
		t.Fatal(err)
	}
	d := quietSite(t).AddDevice(fabrictest.Topology{
		Bridged: []node.Endpoint{light, contact},
		Configure: func(c *node.Config) {
			c.ICD = &node.ICDConfig{
				IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
				ActiveModeDuration: 2000 * time.Millisecond, ActiveModeThreshold: 1000 * time.Millisecond,
				ClientsSupportedPerFabric: 2,
			}
		},
	})
	featureMaps := map[uint32][]uint32{}
	for _, ep := range d.Node().Bridge().Topology().Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
			fm, isUint32 := v.(uint32)
			if !ok || !isUint32 {
				t.Errorf("endpoint %d cluster 0x%04X answers FeatureMap %v (%v)", ep.ID, srv.MatterClusterID(), v, ok)
				continue
			}
			featureMaps[srv.MatterClusterID()] = append(featureMaps[srv.MatterClusterID()], fm)
		}
	}
	for _, row := range []struct {
		behavior string
		id       uint32
		enabled  uint32
	}{
		{"AccessControlServer", accesscontroldef.ClusterID, uint32(accesscontroldef.FeatureExtension)},
		{"BooleanStateServer", booleanstatedef.ClusterID, uint32(booleanstatedef.FeatureChangeEvent)},
		{"GeneralDiagnosticsServer", generaldiagnosticsdef.ClusterID, uint32(generaldiagnosticsdef.FeatureDataModelTest)},
		{"GroupcastServer", groupcastdef.ClusterID, uint32(groupcastdef.FeatureListener | groupcastdef.FeatureSender | groupcastdef.FeaturePerGroup)},
		{"GroupsServer", groupsdef.ClusterID, uint32(groupsdef.FeatureGroupNames)},
		{"IcdManagementServer", icdmanagementdef.ClusterID, uint32(icdmanagementdef.FeatureCheckInProtocolSupport)},
		{"ScenesManagementServer", scenesmanagementdef.ClusterID, uint32(scenesmanagementdef.FeatureSceneNames)},
		{"TimeFormatLocalizationServer", timeformatlocalizationdef.ClusterID, uint32(timeformatlocalizationdef.FeatureCalendarFormat)},
		{"UnitLocalizationServer", unitlocalizationdef.ClusterID, uint32(unitlocalizationdef.FeatureTemperatureUnit)},
	} {
		got := featureMaps[row.id]
		if len(got) == 0 {
			t.Errorf("%s: no default server of cluster 0x%04X served", row.behavior, row.id)
			continue
		}
		for _, fm := range got {
			if row.id == accesscontroldef.ClusterID {
				// The node registers an auxiliary entry provider (the
				// groupcast state, node.go AttachAuxiliaryACL), which turns
				// Auxiliary on (cluster/core access_control_auxiliary.go
				// featureMap): a runtime registration, not the default
				// selection the table states.
				fm &^= uint32(accesscontroldef.FeatureAuxiliary)
			}
			if fm != row.enabled {
				t.Errorf("%s: FeatureMap %s, matter.js enables %s", row.behavior, featureHex(fm), featureHex(row.enabled))
			}
		}
	}
}

func featureHex(v uint32) string { return fmt.Sprintf("0x%X", v) }
