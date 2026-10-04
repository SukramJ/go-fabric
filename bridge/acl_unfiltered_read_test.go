// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestAccessControlUnfilteredReadsRedactOtherFabrics: Acl and Extension
// are fabric-scoped lists whose entries carry fabric-sensitive fields. A
// read with isFabricFiltered=false returns every fabric's entries — the
// accessing fabric's whole, another fabric's with FabricIndex alone — and
// a fabric-filtered read the accessing fabric's only. Mirrors matter.js
// ListManager createProxy (no fabric filter for an unfiltered read of a
// list that is not itself fabric-sensitive), StructManager plus
// protocol/src/action/server/AccessControl.ts mayRead (a fabric-sensitive
// field of an entry another fabric owns reads as undefined) and
// InteractionMessenger, which encodes such a read without the missing
// fields. The read still needs Administer.
func TestAccessControlUnfilteredReadsRedactOtherFabrics(t *testing.T) {
	t.Parallel()
	var acl *mattercore.AccessControl
	h := newSecureHarness(t, nil, func(st *store.Store, _ uint8) []contract.ClusterServer {
		var err error
		if acl, err = mattercore.NewAccessControl(st); err != nil {
			t.Fatal(err)
		}
		return []contract.ClusterServer{acl}
	})
	h.allowAll()
	ctx := context.Background()

	other := addHarnessFabric(t, h.store, 0x0000_0000_0000_0BAD, [8]byte{9, 9, 9, 9, 9, 9, 9, 9})
	if err := h.store.ReplaceACL(ctx, other, []store.ACLEntry{
		{FabricIndex: other, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0xDEAD}},
		{FabricIndex: other, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup, Subjects: []uint64{0x0101}},
	}); err != nil {
		t.Fatal(err)
	}
	for fabric, data := range map[uint8][]byte{h.fabric: {0x17, 0x18}, other: {0x17, 0x24, 0x01, 0x07, 0x18}} {
		fctx := im.WithFabricFilter(ctx, true, fabric)
		if err := acl.MatterWrite(fctx, 0x0001, []mattercore.AccessControlExtensionEntry{{Data: data}}); err != nil {
			t.Fatalf("Extension write for fabric %d: %v", fabric, err)
		}
	}

	for _, attr := range []uint32{0x0000, 0x0001} { // Acl, Extension
		filtered := aclRead(t, h, attr, true)
		if !slices.Equal(fabricIndexesOf(t, filtered), []uint64{uint64(h.fabric)}) {
			t.Errorf("attribute 0x%04X fabric-filtered: fabrics %v, want only %d", attr, fabricIndexesOf(t, filtered), h.fabric)
		}
		all := aclRead(t, h, attr, false)
		want := make([]uint64, 1, len(all.Children))
		want[0] = uint64(h.fabric)
		for range len(all.Children) - 1 {
			want = append(want, uint64(other))
		}
		if !slices.Equal(fabricIndexesOf(t, all), want) || len(all.Children) < 2 {
			t.Fatalf("attribute 0x%04X unfiltered: fabrics %v, want %v", attr, fabricIndexesOf(t, all), want)
		}
		// The accessing fabric's entry goes out whole; another
		// fabric's carries FabricIndex and nothing else.
		if len(all.Children[0].Children) < 2 {
			t.Errorf("attribute 0x%04X: own entry lost its fields: %+v", attr, all.Children[0])
		}
		for _, foreign := range all.Children[1:] {
			if len(foreign.Children) != 1 {
				t.Errorf("attribute 0x%04X: foreign entry carries %d fields, want FabricIndex alone: %+v", attr, len(foreign.Children), foreign)
			}
		}
	}
	if n := len(aclRead(t, h, 0x0000, false).Children); n != 3 {
		t.Errorf("unfiltered Acl carries %d entries, want 1 own + 2 foreign", n)
	}

	// Acl and Extension are "RW F A": an Operate subject reads neither,
	// filtered or not.
	if err := h.store.ReplaceACL(ctx, h.fabric, []store.ACLEntry{{
		FabricIndex: h.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{harnessControllerNodeID},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []uint32{0x0000, 0x0001} {
		if _, status, isStatus := h.readAttribute(0, aclClusterID, attr); !isStatus || status != im.StatusUnsupportedAccess {
			t.Errorf("Operate read of 0x%04X = status %v (isStatus %v), want UnsupportedAccess", attr, status, isStatus)
		}
	}
}

// aclRead reads one AccessControl attribute on the root endpoint.
func aclRead(t *testing.T, h *secureHarness, attr uint32, fabricFiltered bool) tlvNode {
	t.Helper()
	return h.readAttributeData(t, 0, aclClusterID, attr, fabricFiltered)
}

// aclClusterID is AccessControl (access-control.element.ts).
const aclClusterID uint32 = 0x001F
