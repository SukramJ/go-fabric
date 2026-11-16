// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import "testing"

// TestDeviceTypeListedTwiceIsNoDuplicate: an endpoint whose device type
// list names its application device type twice, under a parent with no
// other child, holds no Duplicate condition and owes no TagList — before,
// each list entry counted as a sibling's (review II, R2-P6-par-0660).
func TestDeviceTypeListedTwiceIsNoDuplicate(t *testing.T) {
	t.Parallel()
	std := deviceTypeLookups()
	f, root := nodeTree()
	solo := light(f, root, "solo", dtOnOffLight)
	pass := newDTPass(f, std)
	if pass.isDuplicate(solo.id) {
		t.Error("an endpoint that lists its device type twice is a duplicate of itself")
	}
	if named := requirementsNamed(pass.check(solo.id), "Descriptor.TAGLIST"); len(named) != 0 {
		t.Errorf("TagList owed by an endpoint without a sibling: %v", named)
	}
	// Two siblings that list it are duplicates still.
	light(f, root, "sibling")
	if !newDTPass(f, std).isDuplicate(solo.id) {
		t.Error("an endpoint with a sibling of its device type is no duplicate")
	}
}
