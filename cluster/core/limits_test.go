// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestAccessControlSetLimits: the capacity attributes report what SetLimits
// set, a zero field keeps the default, and an ACL write is held to the set
// limit — ResourceExhausted one past it, as matter.js
// AccessControlServer.ts:179-198 at 862ad75f throws.
func TestAccessControlSetLimits(t *testing.T) {
	t.Parallel()
	ac := newAccessControl(t)
	ac.SetCurrentFabric(1)
	ac.SetLimits(core.AccessControlLimits{EntriesPerFabric: 6, TargetsPerEntry: 5})
	for _, tc := range []struct {
		attr uint32
		want uint16
	}{
		{0x0002, core.DefaultSubjectsPerAccessControlEntry},
		{0x0003, 5},
		{0x0004, 6},
	} {
		v, ok := ac.MatterRead(tc.attr)
		if !ok || v.(uint16) != tc.want {
			t.Errorf("attribute %#x = %v (%v), want %d", tc.attr, v, ok, tc.want)
		}
	}
	entries := func(n int) []core.AccessControlEntryStruct {
		out := make([]core.AccessControlEntryStruct, n)
		for i := range out {
			out[i] = core.AccessControlEntryStruct{Privilege: 5, AuthMode: 2, Subjects: []uint64{uint64(i + 1)}, FabricIndex: 1}
		}
		return out
	}
	if err := writeACL(ac, entries(6)); err != nil {
		t.Fatalf("six entries under a limit of six: %v", err)
	}
	err := writeACL(ac, entries(7))
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusResourceExhausted {
		t.Errorf("seven entries under a limit of six = %v, want ResourceExhausted", err)
	}
}

// TestLimitDefaults: the exported defaults are matter.js's
// (AccessControlServer.ts:58-66, GroupKeyManagementServer.ts:609-610 at
// 862ad75f).
func TestLimitDefaults(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ got, want uint16 }{
		"AccessControlEntriesPerFabric": {core.DefaultAccessControlEntriesPerFabric, 4},
		"SubjectsPerAccessControlEntry": {core.DefaultSubjectsPerAccessControlEntry, 4},
		"TargetsPerAccessControlEntry":  {core.DefaultTargetsPerAccessControlEntry, 4},
		"MaxGroupsPerFabric":            {core.DefaultMaxGroupsPerFabric, 22},
		"MaxGroupKeysPerFabric":         {core.DefaultMaxGroupKeysPerFabric, 20},
	} {
		if tc.got != tc.want {
			t.Errorf("%s default = %d, want %d", name, tc.got, tc.want)
		}
	}
}
