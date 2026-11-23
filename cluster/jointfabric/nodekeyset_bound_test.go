// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	dsdef "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
)

// TestNodeKeySetListIsBounded (R2-C2-bound-1101): UpdateGroup with a fresh
// GroupKeySetId adds a key set entry for every member and erases only the
// first old one, so the list grew without a bound. It now has a capacity:
// the UpdateGroup that would pass it is ErrNoMemory and changes nothing.
func TestNodeKeySetListIsBounded(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, newTrackingSync())
	mustOK(t, d.AddGroup(ctx, addGroup(0x10, nullU16())))
	for n := uint64(1); n <= 4; n++ {
		d.addEndpointEntry(ctx, 1, n, "e")
		mustOK(t, d.AddGroupIDToEndpointForNode(ctx, n, 1, 0x10))
	}
	var err error
	ks := uint16(0)
	for ks < 2000 && err == nil {
		ks++
		err = d.UpdateGroup(ctx, dsdef.UpdateGroupRequest{
			GroupId: 0x10, FriendlyName: spec.Nullable[string]{Null: true}, GroupKeySetId: spec.ValueOf(ks),
			GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: spec.Nullable[dsdef.DatastoreAccessControlEntryPrivilegeEnum]{Null: true},
		})
	}
	if !errors.Is(err, ErrNoMemory) {
		t.Fatalf("after %d UpdateGroups: %v, NodeKeySetList %d; want ErrNoMemory at the capacity", ks, err, len(d.NodeKeySetList()))
	}
	if n := len(d.NodeKeySetList()); n > maxNodeKeySets {
		t.Errorf("NodeKeySetList holds %d, above %d", n, maxNodeKeySets)
	}
	if g := d.GroupList()[0]; g.GroupKeySetId.Null || g.GroupKeySetId.Value != ks-1 {
		t.Errorf("the refused UpdateGroup changed the group's key set to %+v", g.GroupKeySetId)
	}
}
