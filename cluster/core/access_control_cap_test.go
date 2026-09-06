// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestAccessControl_ACLCapCountsEveryEntryWhateverFabricIndexItCarries
// pins the per-fabric limit against the list as it will be stored, not
// as the client labelled it. Every entry in a write lands on the
// writer's fabric (the stamp in MatterWrite), so a list of five entries
// tagged with a foreign FabricIndex is five entries on this fabric.
// Counting only the entries whose tag matched let that list through and
// persisted all five, while AccessControlEntriesPerFabric kept reporting
// four. Mirrors the count matter.js reaches after its fabric-scoped
// write has stamped the accessing fabric
// (AccessControlServer.ts:186-189).
func TestAccessControl_ACLCapCountsEveryEntryWhateverFabricIndexItCarries(t *testing.T) {
	t.Parallel()
	ac := newAccessControl(t)
	ac.SetCurrentFabric(1)

	entries := make([]core.AccessControlEntryStruct, 5)
	for i := range entries {
		entries[i] = core.AccessControlEntryStruct{
			Privilege:   5,
			AuthMode:    2,
			Subjects:    []uint64{uint64(i + 1)},
			FabricIndex: 200, // not the writer's fabric — raw wire input
		}
	}
	err := writeACL(ac, entries)
	if err == nil {
		t.Fatal("five entries tagged with a foreign FabricIndex were accepted; the per-fabric cap must count the list that is stored")
	}
	for _, want := range []string{"resource exhausted", "AccessControlEntriesPerFabric"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
