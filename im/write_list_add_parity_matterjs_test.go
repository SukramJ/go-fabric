// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// TestListAddMarkerParityMatterJS: a list ADD (null list index) is valid
// only right after a write path of the same attribute in the same write
// interaction — across its chunks — and is answered BUSY otherwise, before
// anything else is checked, leaving the marker where it was (matter.js
// packages/node/src/node/server/InteractionServer.ts:511-531, the marker
// previousProcessedAttributePath :456-458 kept across chunks, unchanged by
// a BUSY path :529, at db021da0; issue 438).
func TestListAddMarkerParityMatterJS(t *testing.T) {
	t.Parallel()
	nodeLabel, location := holdAttr(holdNodeLabel), holdAttr(holdLocation)
	add := func(p ConcreteAttributePath) ConcreteAttributePath {
		p.ListAppend = true
		return p
	}
	wildcardAdd := add(nodeLabel)
	wildcardAdd.HasEndpoint = false

	t.Run("answers a lone ADD with BUSY and does not write it", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		wantStatuses(t, "lone ADD", exchange().write(false, holdWrite{add(nodeLabel), "x"}), StatusBusy)
		if _, ok := d.values[holdNodeLabel]; ok {
			t.Fatal("a BUSY ADD was written")
		}
	})

	t.Run("accepts an ADD after a write of the same attribute in the same message", func(t *testing.T) {
		t.Parallel()
		_, exchange := newHoldNode()
		wantStatuses(t, "replace then ADD", exchange().write(false,
			holdWrite{nodeLabel, ""}, holdWrite{add(nodeLabel), "a"}, holdWrite{add(nodeLabel), "b"}),
			StatusSuccess, StatusSuccess, StatusSuccess)
	})

	t.Run("keeps the marker across the chunks of one interaction", func(t *testing.T) {
		t.Parallel()
		_, exchange := newHoldNode()
		x := exchange()
		wantStatuses(t, "first chunk", x.write(true, holdWrite{nodeLabel, ""}), StatusSuccess)
		wantStatuses(t, "second chunk", x.write(false, holdWrite{add(nodeLabel), "a"}), StatusSuccess)
		// A new interaction starts without a marker.
		wantStatuses(t, "next interaction", exchange().write(false, holdWrite{add(nodeLabel), "b"}), StatusBusy)
	})

	t.Run("answers an ADD after another attribute's write with BUSY, and the BUSY leaves the marker", func(t *testing.T) {
		t.Parallel()
		_, exchange := newHoldNode()
		wantStatuses(t, "ADD after another attribute", exchange().write(false,
			holdWrite{location, "XX"}, holdWrite{add(nodeLabel), "a"}, holdWrite{add(location), "YY"}),
			StatusSuccess, StatusBusy, StatusSuccess)
	})

	t.Run("answers an ADD without an endpoint with BUSY", func(t *testing.T) {
		t.Parallel()
		_, exchange := newHoldNode()
		wantStatuses(t, "wildcard ADD", exchange().write(false, holdWrite{nodeLabel, ""}, holdWrite{wildcardAdd, "a"}),
			StatusSuccess, StatusBusy)
	})
}
