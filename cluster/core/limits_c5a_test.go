// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
)

// TestNewAccessControlWithJudgesTheLimitsA1 (R4-api-4157, decision 669):
// the config constructor takes the capacity limits with the store and
// refuses, at construction, a limit outside its range — EntriesPerFabric
// and SubjectsPerEntry 4 to 65534, TargetsPerEntry 3 to 65534 — so a node
// never advertises a capacity its own constraint forbids; a zero field is
// its default, and a store is required.
func TestNewAccessControlWithJudgesTheLimitsA1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := core.NewAccessControlWith(ctx, core.AccessControlConfig{}); err == nil {
		t.Error("a config without a store was accepted")
	}
	for name, l := range map[string]core.AccessControlLimits{
		"EntriesPerFabric 3":     {EntriesPerFabric: 3},
		"SubjectsPerEntry 3":     {SubjectsPerEntry: 3},
		"TargetsPerEntry 2":      {TargetsPerEntry: 2},
		"EntriesPerFabric 65535": {EntriesPerFabric: 65535},
		"SubjectsPerEntry 65535": {SubjectsPerEntry: 65535},
		"TargetsPerEntry 65535":  {TargetsPerEntry: 65535},
	} {
		if _, err := core.NewAccessControlWith(ctx, core.AccessControlConfig{Store: newFakeStore(), Limits: l}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, tc := range map[string]struct {
		limits core.AccessControlLimits
		want   [3]uint16 // SubjectsPerEntry, TargetsPerEntry, EntriesPerFabric
	}{
		"zero, the defaults": {core.AccessControlLimits{}, [3]uint16{
			core.DefaultSubjectsPerAccessControlEntry, core.DefaultTargetsPerAccessControlEntry, core.DefaultAccessControlEntriesPerFabric,
		}},
		"the floors":   {core.AccessControlLimits{EntriesPerFabric: 4, SubjectsPerEntry: 4, TargetsPerEntry: 3}, [3]uint16{4, 3, 4}},
		"the ceilings": {core.AccessControlLimits{EntriesPerFabric: 65534, SubjectsPerEntry: 65534, TargetsPerEntry: 65534}, [3]uint16{65534, 65534, 65534}},
	} {
		ac, err := core.NewAccessControlWith(ctx, core.AccessControlConfig{Store: newFakeStore(), Limits: tc.limits})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for i, attr := range []uint32{0x0002, 0x0003, 0x0004} {
			if v, ok := ac.MatterRead(attr); !ok || v.(uint16) != tc.want[i] {
				t.Errorf("%s: attribute %#x = %v (%v), want %d", name, attr, v, ok, tc.want[i])
			}
		}
	}
}
