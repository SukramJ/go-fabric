// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// A session with no fabric yet — PASE before AddNOC, FabricIndex 0 — owns
// no entry of any fabric-scoped list. matter.js gives it nothing
// fabric-sensitive:
//
//   - a fabric-filtered read is filtered to `session.fabric`, which it does
//     not have, so every fabric-scoped list reads empty
//     (packages/node/src/behavior/state/managed/values/ListManager.ts
//     FabricFilteredListProxyHandler);
//   - an unfiltered read lists every fabric's entries, but a field with
//     the fabric-sensitive ("S") access reads as absent, because mayRead
//     refuses it to a session without `session.fabric`
//     (packages/protocol/src/action/server/AccessControl.ts
//     dataEnforcerFor, limits.fabricSensitive);
//   - OperationalCredentials CurrentFabricIndex is `session.fabric ??
//     FabricIndex.NO_FABRIC` (OperationalCredentialsServer.ts State
//     [Val.properties]).
//
// A read with no request behind it is a local actor, which matter.js lets
// see everything (AccessControl.ts hasLocalActor); the tests below hold
// the two apart.

// paseRequest is the context of a request on a PASE session that has no
// fabric yet.
func paseRequest(filtered bool) context.Context {
	return im.WithAuthModePASE(im.WithSubject(im.WithFabricFilter(context.Background(), filtered, 0), 0, nil))
}

// fabriclessFixture is two fabrics, each with an ACL entry, an Extension,
// a Groupcast membership with an auxiliary ACL (which also creates a
// GroupKeyMap entry and a group table entry) and a NOC.
type fabriclessFixture struct {
	*gcFixture
	oc *core.OperationalCredentials
}

func newFabriclessFixture(t *testing.T) *fabriclessFixture {
	t.Helper()
	f := &fabriclessFixture{gcFixture: newGCFixture(t, 2)}
	ctx := context.Background()
	for _, fabric := range []uint8{1, 2} {
		if err := f.fs.ReplaceACL(ctx, fabric, []mstore.ACLEntry{{
			FabricIndex: fabric, Privilege: mstore.PrivilegeAdminister, AuthMode: mstore.AuthModeCASE,
			Subjects: []uint64{0x1000 + uint64(fabric)},
		}}); err != nil {
			t.Fatal(err)
		}
		if err := f.acl.MatterWrite(im.WithFabricFilter(ctx, true, fabric), 0x0001,
			[]core.AccessControlExtensionEntry{{Data: []byte{0x17, 0x18}}}); err != nil {
			t.Fatalf("Extension write for fabric %d: %v", fabric, err)
		}
		f.join(as(fabric, 5), t, core.JoinGroupRequest{
			GroupID: uint16(fabric), Endpoints: []uint16{3}, KeySetID: uint16(fabric), Key: testKey, UseAuxiliaryACL: boolp(true),
		})
		if err := f.fs.UpsertIdentity(ctx, mstore.IdentityRecord{
			FabricIndex: fabric, NOC: []byte{0x15, fabric, 0x18}, ICAC: []byte{0x15, 0x18},
		}); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	if f.oc, err = core.NewOperationalCredentials(f.fs, core.OpcredsConfig{SupportedFabrics: 5}); err != nil {
		t.Fatal(err)
	}
	// The legacy hooks a local write is scoped by. Neither a fabric-less
	// request nor a local read may fall back to them.
	f.acl.SetCurrentFabric(1)
	f.gkm.SetCurrentFabric(1)
	return f
}

// fabricScopedReader is the read side under test.
type fabricScopedReader interface {
	MatterReadFiltered(context.Context, uint32) (any, bool)
}

// read reads one attribute of a fabric-scoped reader.
func read[T any](ctx context.Context, t *testing.T, r fabricScopedReader, attr uint32) []T {
	t.Helper()
	v, ok := r.MatterReadFiltered(ctx, attr)
	if !ok {
		t.Fatalf("attribute 0x%04X unreadable", attr)
	}
	list, ok := v.([]T)
	if !ok {
		t.Fatalf("attribute 0x%04X is %T", attr, v)
	}
	return list
}

func TestFabricFilteredReadOnFabriclessSessionIsEmpty(t *testing.T) {
	t.Parallel()
	f := newFabriclessFixture(t)
	ctx := paseRequest(true)

	cases := map[string]int{
		"AccessControl.Acl":              len(read[core.AccessControlEntryStruct](ctx, t, f.acl, 0x0000)),
		"AccessControl.Extension":        len(read[core.AccessControlExtensionEntry](ctx, t, f.acl, 0x0001)),
		"AccessControl.AuxiliaryAcl":     len(read[core.AccessControlAuxiliaryEntryStruct](ctx, t, f.acl, 0x0007)),
		"OperationalCredentials.NOCs":    len(read[core.NOCStruct](ctx, t, f.oc, 0x0000)),
		"OperationalCredentials.Fabrics": len(read[core.FabricDescriptorStruct](ctx, t, f.oc, 0x0001)),
		"GroupKeyManagement.GroupKeyMap": len(read[core.GroupKeyMapStruct](ctx, t, f.gkm, 0x0000)),
		"GroupKeyManagement.GroupTable":  len(read[core.GroupInfoMapStruct](ctx, t, f.gkm, 0x0001)),
		"Groupcast.Membership":           len(read[core.GroupcastMembershipStruct](ctx, t, f.gc, 0x0000)),
	}
	for name, n := range cases {
		if n != 0 {
			t.Errorf("%s: fabric-less fabric-filtered read carries %d entries, want none", name, n)
		}
	}
}

func TestUnfilteredReadOnFabriclessSessionWithholdsFabricSensitiveFields(t *testing.T) {
	t.Parallel()
	f := newFabriclessFixture(t)
	ctx := paseRequest(false)

	// AccessControl Acl / Extension / AuxiliaryAcl: every fabric's
	// entries, each read as its FabricIndex alone — every other field of
	// their structs is fabric-sensitive. Groupcast Membership: every
	// fabric's, KeySetId (2, "S") withheld. The servers return the entries
	// whole; the dispatcher scopes them ([readWire]).
	for _, c := range []struct {
		name string
		srv  contract.ClusterServer
		attr uint32
		want []uint32
	}{
		{"Acl", f.acl, 0x0000, []uint32{0xFE}},
		{"Extension", f.acl, 0x0001, []uint32{0xFE}},
		{"AuxiliaryAcl", f.acl, 0x0007, []uint32{0xFE}},
		{"Membership", f.gc, 0x0000, []uint32{0, 1, 3, 4, 0xFE}},
	} {
		entries := wireEntries(t, readWire(ctx, t, c.srv, c.attr))
		fabrics := make([]uint8, 0, len(entries))
		for _, e := range entries {
			fabrics = append(fabrics, e.fabric)
			if !slices.Equal(e.tags, c.want) {
				t.Errorf("%s entry of fabric %d goes to a fabric-less session with fields %v, want %v", c.name, e.fabric, e.tags, c.want)
			}
		}
		if !slices.Equal(fabrics, []uint8{1, 2}) {
			t.Errorf("%s fabrics %v, want [1 2]", c.name, fabrics)
		}
	}

	// No fabric-sensitive field in these: every fabric's entries whole.
	nocs := read[core.NOCStruct](ctx, t, f.oc, 0x0000)
	if got := fabricsOf(nocs, func(n core.NOCStruct) uint8 { return n.FabricIndex }); !slices.Equal(got, []uint8{1, 2}) || nocs[0].NOC == nil {
		t.Errorf("NOCs %+v, want both fabrics whole", nocs)
	}
	if fabrics := read[core.FabricDescriptorStruct](ctx, t, f.oc, 0x0001); len(fabrics) != 2 {
		t.Errorf("Fabrics carries %d entries, want 2", len(fabrics))
	}
	gkm := read[core.GroupKeyMapStruct](ctx, t, f.gkm, 0x0000)
	if got := fabricsOf(gkm, func(m core.GroupKeyMapStruct) uint8 { return m.FabricIndex }); !slices.Equal(got, []uint8{1, 2}) {
		t.Errorf("GroupKeyMap fabrics %v, want [1 2]", got)
	}
	table := read[core.GroupInfoMapStruct](ctx, t, f.gkm, 0x0001)
	if got := fabricsOf(table, func(m core.GroupInfoMapStruct) uint8 { return m.FabricIndex }); !slices.Equal(got, []uint8{1, 2}) {
		t.Errorf("GroupTable fabrics %v, want [1 2]", got)
	}
}

func TestCurrentFabricIndexOnFabriclessSessionIsZero(t *testing.T) {
	t.Parallel()
	f := newFabriclessFixture(t)
	for _, filtered := range []bool{true, false} {
		if v, ok := f.oc.MatterReadFiltered(paseRequest(filtered), 0x0005); !ok || v != uint8(0) {
			t.Errorf("fabric-less CurrentFabricIndex (filtered=%v) = %v, %v; want 0", filtered, v, ok)
		}
	}
	if v, ok := f.oc.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 2), 0x0005); !ok || v != uint8(2) {
		t.Errorf("CASE CurrentFabricIndex = %v, %v; want the session's fabric 2", v, ok)
	}
}

// TestLocalReadSeesEveryFabricWhole pins the other half: a read with no
// request behind it — a host's MatterRead, or MatterReadFiltered with no
// fabric filter stamped, the facade's Node.ReadAttribute — is not a
// fabric-less session but a local actor, and sees every fabric's entries
// whole, whatever the SetCurrentFabric hooks hold (the fixture sets them
// to fabric 1). Mirrors matter.js
// packages/protocol/src/action/server/AccessControl.ts:250-253 and
// :342-345 at db021da0 (mayRead grants a local actor every attribute and
// every fabric-sensitive field) and
// packages/node/src/behavior/state/managed/values/ListManager.ts:609-616
// (createProxy filters a fabric-scoped list for a remote actor only).
func TestLocalReadSeesEveryFabricWhole(t *testing.T) {
	t.Parallel()
	f := newFabriclessFixture(t)
	local := context.Background()
	direct := func(r interface{ MatterRead(uint32) (any, bool) }, attr uint32) any {
		v, ok := r.MatterRead(attr)
		if !ok {
			t.Fatalf("MatterRead(0x%04X) unreadable", attr)
		}
		return v
	}

	for name, acl := range map[string][]core.AccessControlEntryStruct{
		"MatterReadFiltered": read[core.AccessControlEntryStruct](local, t, f.acl, 0x0000),
		"MatterRead":         direct(f.acl, 0x0000).([]core.AccessControlEntryStruct),
	} {
		if got := fabricsOf(acl, func(e core.AccessControlEntryStruct) uint8 { return e.FabricIndex }); len(acl) != 2 || !slices.Equal(got, []uint8{1, 2}) {
			t.Errorf("local Acl read (%s) = %+v, want one entry of each fabric", name, acl)
		}
		for _, e := range acl {
			if e.Redacted || e.Privilege != uint8(mstore.PrivilegeAdminister) || len(e.Subjects) != 1 || e.Subjects[0] != 0x1000+uint64(e.FabricIndex) {
				t.Errorf("local Acl read (%s): the entry of fabric %d is not whole: %+v", name, e.FabricIndex, e)
			}
		}
	}
	for name, ext := range map[string][]core.AccessControlExtensionEntry{
		"MatterReadFiltered": read[core.AccessControlExtensionEntry](local, t, f.acl, 0x0001),
		"MatterRead":         direct(f.acl, 0x0001).([]core.AccessControlExtensionEntry),
	} {
		if got := fabricsOf(ext, func(e core.AccessControlExtensionEntry) uint8 { return e.FabricIndex }); len(ext) != 2 || !slices.Equal(got, []uint8{1, 2}) {
			t.Errorf("local Extension read (%s) = %+v, want one entry of each fabric", name, ext)
		}
		for _, e := range ext {
			if e.Redacted || e.Data == nil {
				t.Errorf("local Extension read (%s): the entry of fabric %d is not whole: %+v", name, e.FabricIndex, e)
			}
		}
	}
	for name, gkm := range map[string][]core.GroupKeyMapStruct{
		"MatterReadFiltered": read[core.GroupKeyMapStruct](local, t, f.gkm, 0x0000),
		"MatterRead":         direct(f.gkm, 0x0000).([]core.GroupKeyMapStruct),
	} {
		if got := fabricsOf(gkm, func(m core.GroupKeyMapStruct) uint8 { return m.FabricIndex }); !slices.Equal(got, []uint8{1, 2}) {
			t.Errorf("local GroupKeyMap read (%s) covers fabrics %v, want [1 2]", name, got)
		}
	}
	for name, table := range map[string][]core.GroupInfoMapStruct{
		"MatterReadFiltered": read[core.GroupInfoMapStruct](local, t, f.gkm, 0x0001),
		"MatterRead":         direct(f.gkm, 0x0001).([]core.GroupInfoMapStruct),
	} {
		if got := fabricsOf(table, func(m core.GroupInfoMapStruct) uint8 { return m.FabricIndex }); !slices.Equal(got, []uint8{1, 2}) {
			t.Errorf("local GroupTable read (%s) covers fabrics %v, want [1 2]", name, got)
		}
	}
}

func fabricsOf[T any](list []T, fabric func(T) uint8) []uint8 {
	out := make([]uint8, 0, len(list))
	for _, e := range list {
		out = append(out, fabric(e))
	}
	slices.Sort(out)
	return slices.Compact(out)
}
