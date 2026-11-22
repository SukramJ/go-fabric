// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/surface"
)

// TestDiffNamesADeviceTypeOrderOrDuplicateChange holds Diff to the digest:
// a DeviceTypeList that a controller can tell apart — another order, a
// duplicate entry — is a difference Diff names. Before, Diff compared the
// list as a set: a reorder changed the digest and announced a
// SurfaceChange with no Change, and an added duplicate with another
// revision read as a revision change of the entry already there (review
// II, R2-P6-api-1362).
func TestDiffNamesADeviceTypeOrderOrDuplicateChange(t *testing.T) {
	t.Parallel()
	a := surface.Endpoint{Key: "k", DeviceTypes: []surface.DeviceTypeRef{{ID: 0x0100, Revision: 3}, {ID: 0x0013, Revision: 3}}}
	diff := func(b surface.Endpoint) []surface.Change {
		return surface.Diff(surface.Surface{Endpoints: []surface.Endpoint{a}}, surface.Surface{Endpoints: []surface.Endpoint{b}})
	}

	swapped := a.Clone()
	swapped.DeviceTypes[0], swapped.DeviceTypes[1] = swapped.DeviceTypes[1], swapped.DeviceTypes[0]
	if a.Digest() == swapped.Digest() {
		t.Fatal("a reorder leaves the digest equal: the case this test holds no longer occurs")
	}
	want := []surface.Change{
		{Endpoint: "k", Kind: surface.KindDeviceType, ID: 0x0013, Before: "position 1", After: "position 0"},
		{Endpoint: "k", Kind: surface.KindDeviceType, ID: 0x0100, Before: "position 0", After: "position 1"},
	}
	if got := diff(swapped); !slices.Equal(got, want) {
		t.Errorf("reorder: Diff = %v, want %v", got, want)
	}

	dup := a.Clone()
	dup.DeviceTypes = append(dup.DeviceTypes, surface.DeviceTypeRef{ID: 0x0100, Revision: 2})
	want = []surface.Change{{Endpoint: "k", Kind: surface.KindDeviceType, ID: 0x0100, Before: "revision 3", After: "revision 3, revision 2"}}
	if got := diff(dup); !slices.Equal(got, want) {
		t.Errorf("duplicate: Diff = %v, want %v", got, want)
	}

	// The same list in the same order is no difference, a revision change
	// reads as before.
	if got := diff(a.Clone()); len(got) != 0 {
		t.Errorf("equal: Diff = %v, want none", got)
	}
	rev := a.Clone()
	rev.DeviceTypes[0].Revision = 4
	want = []surface.Change{{Endpoint: "k", Kind: surface.KindDeviceType, ID: 0x0100, Before: "revision 3", After: "revision 4"}}
	if got := diff(rev); !slices.Equal(got, want) {
		t.Errorf("revision: Diff = %v, want %v", got, want)
	}
}
