// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"testing"

	dsdef "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	"thingfabric.dev/go-fabric/contract"
)

// TestDatastoreDeclaresItsReadAccess (decision 152, R2-P6-sec-1103 read
// half): the Datastore is a contract.ReadAccessDeclarer that admits a
// request's read of a cluster attribute only from the anchor fabric —
// none while no anchor is recorded — and the universal globals to every
// request; a read without a request (the host) is admitted.
func TestDatastoreDeclaresItsReadAccess(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	var decl contract.ReadAccessDeclarer = d
	for id := range attrIDs() {
		if decl.MatterReadAllowed(peerOf(1), id) {
			t.Errorf("attribute 0x%04X: admitted without an anchor", id)
		}
	}
	anchorAt(t, d, 1)
	for id := range attrIDs() {
		if decl.MatterReadAllowed(peerOf(2), id) {
			t.Errorf("attribute 0x%04X: fabric 2 admitted", id)
		}
		if !decl.MatterReadAllowed(peerOf(1), id) {
			t.Errorf("attribute 0x%04X: the anchor fabric refused", id)
		}
		if !decl.MatterReadAllowed(ctx, id) {
			t.Errorf("attribute 0x%04X: the host refused", id)
		}
	}
	for _, g := range []uint32{0xFFF8, 0xFFF9, 0xFFFB, 0xFFFC, 0xFFFD} {
		if !decl.MatterReadAllowed(peerOf(2), g) {
			t.Errorf("global 0x%04X refused to fabric 2", g)
		}
	}
	if decl.MatterReadAllowed(peerOf(2), dsdef.AttrGroupKeySetList) {
		t.Error("fabric 2 may read GroupKeySetList")
	}
}
