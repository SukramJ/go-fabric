// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"testing"

	"thingfabric.dev/go-fabric/schema"
)

// TestUnknownNetworkCommissioningFeaturesLeaveInterfacesUnknown: a client
// view whose NetworkCommissioning FeatureMap has not arrived answers the
// network-interface conditions as unknown, not false, so a requirement
// whose conformance names one — here a synthetic server mandated by
// "!Ethernet" — is not judged; once the FeatureMap arrives it is (review
// II, R2-P6-err-0661). Before, Ethernet read as false and the requirement
// was judged on data the view did not have.
func TestUnknownNetworkCommissioningFeaturesLeaveInterfacesUnknown(t *testing.T) {
	t.Parallel()
	const wiredID, switchID uint32 = 0x7ff3, 0xfff1_0010
	notEthernet := schema.Conformance{Text: "!Ethernet", Op: "!", Args: []schema.Conformance{confName(condEthernet)}}
	l := syntheticModel(
		&schema.DeviceTypeDefinition{
			Name: "Base", Classification: "base", Conditions: []string{condEthernet, condWiFi, condThread},
			Requirements: []schema.DeviceTypeRequirement{clusterReq("Wired", wiredID, notEthernet)},
		},
		&schema.DeviceTypeDefinition{ID: dtRootNode, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: switchID, Name: "Switch", Classification: "simple"},
	)
	missingWired := func(nc ClusterShape) bool {
		root := &plainSubject{
			number: 0, types: []uint32{dtRootNode}, servers: []uint32{networkCommissioningClusterID, wiredID},
			parts: []uint16{1}, shapes: map[uint32]ClusterShape{networkCommissioningClusterID: nc},
		}
		sw := &plainSubject{number: 1, types: []uint32{switchID}, shapes: map[uint32]ClusterShape{}}
		pass := newDTPass(newSubjectFacts(treeOf([]*plainSubject{root, sw}, nil)), l)
		return len(requirementsNamed(pass.check(1), "Wired")) > 0
	}
	if missingWired(ClusterShape{AttributesKnown: true, CommandsKnown: true, EventsKnown: true}) {
		t.Error("a FeatureMap not yet known judged a requirement on the Ethernet condition")
	}
	// Known, without the Ethernet feature: Ethernet is false, "!Ethernet"
	// mandates the server, and the switch lacks it.
	known := ClusterShape{FeaturesKnown: true, AttributesKnown: true, CommandsKnown: true, EventsKnown: true}
	if !missingWired(known) {
		t.Error("a known FeatureMap without Ethernet did not judge the requirement")
	}
}
