// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestACLWriteRejectsInvalidTargetIDs is the boundary table of the ACL
// write's target-id validation (AccessControl.MatterWriteChanged: the
// reserved endpoint 0xFFFF, aclIsValidClusterID, aclIsValidDeviceTypeID).
// Each id sits on one side of one bound — the last accepted value and the
// first refused one — so moving a bound by one, or dropping one clause of
// a compound check, flips a row (R2-P5-tmut-08). The ranges are the ones
// the functions' comments derive from matter.js ClusterId / DeviceTypeId /
// EndpointNumber; TestAccessControl_TargetClusterAndDeviceTypeValidity
// holds the coarse shape.
func TestACLWriteRejectsInvalidTargetIDs(t *testing.T) {
	t.Parallel()
	ep := func(v uint16) core.ACLTargetStruct { return core.ACLTargetStruct{Endpoint: &v} }
	cl := func(v uint32) core.ACLTargetStruct { return core.ACLTargetStruct{Cluster: &v} }
	dt := func(v uint32) core.ACLTargetStruct { return core.ACLTargetStruct{DeviceType: &v} }
	cases := []struct {
		name   string
		target core.ACLTargetStruct
		ok     bool
	}{
		{"endpoint 0xFFFE", ep(0xFFFE), true},
		{"endpoint 0xFFFF is the reserved wildcard", ep(0xFFFF), false},

		{"standard cluster, last suffix 0x7FFF", cl(0x0000_7FFF), true},
		{"standard cluster, suffix 0x8000", cl(0x0000_8000), false},
		{"vendor cluster, first suffix 0xFC00", cl(0x0001_FC00), true},
		{"vendor cluster, suffix 0xFBFF", cl(0x0001_FBFF), false},
		{"vendor cluster, last suffix 0xFFFE", cl(0x0001_FFFE), true},
		{"vendor cluster, suffix 0xFFFF", cl(0x0001_FFFF), false},
		{"vendor cluster, last prefix 0xFFF4", cl(0xFFF4_FC00), true},
		{"vendor cluster, prefix 0xFFF5", cl(0xFFF5_FC00), false},

		{"device type, last suffix 0xBFFF", dt(0x0000_BFFF), true},
		{"device type, suffix 0xC000", dt(0x0000_C000), false},
		{"device type, last prefix 0xFFF4", dt(0xFFF4_0100), true},
		{"device type, prefix 0xFFF5", dt(0xFFF5_0100), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ac := newAccessControl(t)
			ac.SetCurrentFabric(1)
			err := writeACL(ac, []core.AccessControlEntryStruct{{
				Privilege:   5, // Administer
				AuthMode:    2, // CASE
				Subjects:    []uint64{1},
				Targets:     []core.ACLTargetStruct{tc.target},
				FabricIndex: 1,
			}})
			switch {
			case tc.ok && err != nil:
				t.Fatalf("write refused: %v", err)
			case !tc.ok && statusOf(err) != im.StatusConstraintError:
				t.Fatalf("write = %v (status %v), want CONSTRAINT_ERROR", err, statusOf(err))
			}
		})
	}
}

// extensionList returns a well-formed TLV list of exactly n bytes: one
// context-tagged octet string (control 0x30, tag, 1-byte length) padded to
// size, between the list's 0x17 and its end-of-container 0x18.
func extensionList(t *testing.T, n int) []byte {
	t.Helper()
	pad := n - 5
	if pad < 0 || pad > 0xFF {
		t.Fatalf("extensionList(%d): out of range", n)
	}
	out := append([]byte{0x17, 0x30, 0x01, byte(pad)}, bytes.Repeat([]byte{0xA5}, pad)...)
	return append(out, 0x18)
}

// TestACLExtensionDataBounds pins the Extension write's Data checks
// (AccessControl.MatterWriteChanged's 128-byte limit and
// validateAccessControlExtensionData's shape). The size rows are valid
// TLV lists, so only the length check can refuse the 129-byte one — the
// existing TooLarge test sends 129 zero bytes, which the TLV check
// refuses as well. The shape rows each break one clause of the
// validator: the leading list type, the trailing end-of-container, and
// the member depth that keeps an anonymous member at the list's top
// level from hiding behind a tagged one (R2-P5-tmut-08).
func TestACLExtensionDataBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data []byte
		ok   bool
	}{
		{"128 bytes", extensionList(t, 128), true},
		{"129 bytes", extensionList(t, 129), false},
		{"a structure, not a list", []byte{0x15, 0x24, 0x01, 0x07, 0x18}, false},
		{"a list followed by a second element", []byte{0x17, 0x24, 0x01, 0x07, 0x18, 0x04, 0x01}, false},
		{"an anonymous member after a tagged one", []byte{0x17, 0x24, 0x01, 0x07, 0x04, 0x02, 0x18}, false},
		{"an anonymous member inside a tagged array", []byte{0x17, 0x36, 0x01, 0x04, 0x02, 0x18, 0x24, 0x02, 0x07, 0x18}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ac := newAccessControl(t)
			data := tc.data
			err := ac.MatterWrite(im.WithFabricFilter(context.Background(), true, 1), 0x0001, []core.AccessControlExtensionEntry{{Data: data}})
			switch {
			case tc.ok && err != nil:
				t.Fatalf("Data %s refused: %v", fmt.Sprintf("% X", data), err)
			case !tc.ok && statusOf(err) != im.StatusConstraintError:
				t.Fatalf("Data %s = %v (status %v), want CONSTRAINT_ERROR", fmt.Sprintf("% X", data), err, statusOf(err))
			}
		})
	}
}
