// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

// TestCoreServersDoNotServeEventList pins EventList (0xFFFA) out of the three
// core servers that used to answer it themselves: OperationalCredentials,
// GroupKeyManagement and Groups.
//
// matter.js marks EventList deprecated (packages/model/src/standard/
// elements/event-list.element.ts, conformance "D"), and the dispatcher omits
// it from every synthesised AttributeList and every wildcard expansion
// (endpoint/dispatcher.go, synthesizeGlobalRead / attributesFor). These three
// servers answered it and named it in their AttributeList anyway — Groups
// even named it in AttributeList while leaving it out of MatterAttributes,
// so a wildcard read did not return an attribute AttributeList promised.
// Found by the chip-tool data-model sweep (internal/chiptool,
// testDataModelSweep), which holds every AttributeList against what a
// wildcard read actually returns.
func TestCoreServersDoNotServeEventList(t *testing.T) {
	t.Parallel()
	servers := map[string]contract.ClusterServer{
		"OperationalCredentials": newOpcreds(t),
		"GroupKeyManagement":     newGKM(t),
		"Groups":                 newGroupsFixture(t, 0).ep3,
	}
	for name, srv := range servers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if v, ok := srv.MatterRead(cluster.AttrGlobalEventList); ok {
				t.Errorf("MatterRead(EventList) = %v, true; want unsupported", v)
			}
			list, ok := srv.MatterRead(cluster.AttrGlobalAttributeList)
			if !ok {
				t.Fatal("AttributeList not served")
			}
			if ids, _ := list.([]uint32); slices.Contains(ids, cluster.AttrGlobalEventList) {
				t.Errorf("AttributeList %v names EventList", ids)
			}
			if lister, ok := srv.(contract.ClusterAttributeLister); ok && slices.Contains(lister.MatterAttributes(), cluster.AttrGlobalEventList) {
				t.Errorf("MatterAttributes %v names EventList", lister.MatterAttributes())
			}
		})
	}
}
