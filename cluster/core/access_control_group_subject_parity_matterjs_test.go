// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestAccessControlGroupSubjectsAreGroupIDs pins the Group auth mode's
// subject: a Group ID, 0x0001..0xFFFF — the value a group message's
// Incoming Subject Descriptor carries (matter.js FabricAccessControl
// #getIsdFromMessage: `isd.subjects.push(subject.id)`). matter.js
// AccessControlServer refuses group id 0 with ConstraintError, and GroupId()
// refuses a value beyond 0xFFFF; a Group Node ID (0xFFFF_FFFF_FFFF_xxxx)
// is no group id. The validator used to accept only the Group Node ID
// range, which left every Group entry a controller writes unwritable.
func TestAccessControlGroupSubjectsAreGroupIDs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		subject uint64
		ok      bool
	}{
		{0x0001, true},
		{0x0101, true},
		{0xFFFF, true},
		{0x0000, false},
		{0x1_0000, false},
		{0xFFFF_FFFF_FFFF_FF01, false},
	}
	for _, tc := range cases {
		ac := newAccessControl(t)
		ac.SetCurrentFabric(1)
		err := writeACL(ac, []core.AccessControlEntryStruct{
			{Privilege: 5, AuthMode: 2, Subjects: []uint64{0x1B669}, FabricIndex: 1},
			{Privilege: 3, AuthMode: 3, Subjects: []uint64{tc.subject}, FabricIndex: 1},
		})
		switch {
		case tc.ok && err != nil:
			t.Errorf("Group subject 0x%X refused: %v", tc.subject, err)
		case !tc.ok && (err == nil || !strings.Contains(err.Error(), "constraint error")):
			t.Errorf("Group subject 0x%X: %v, want a constraint error", tc.subject, err)
		}
	}
}
