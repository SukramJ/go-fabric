// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package storetest is the contract test of the persistence port
// [store.Port]. [Run] drives one table of cases against any implementation:
// store runs it against SQLite's [store.Store], store/memstore against
// itself, and a host with a backend of its own runs it against that one —
// a backend that passes behaves, for every consumer in this module, as the
// shipped two do.
//
//	func TestMyBackend(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) store.Port { return mybackend.Open(t.TempDir()) })
//	}
//
// The cases check what the port's documentation promises (store/port.go):
// round trips, the not-found sentinels, the ordering of every list, the
// fabric cascade, the forward-only fabric-index counter, the refusal of rows
// for an unknown fabric, copies on read and write, and safety under
// concurrent use. They do not check SQLite's encodings or timestamps.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// Open returns a fresh, empty store for one case. It may register cleanups
// on t.
type Open func(t *testing.T) store.Port

// Run runs every case against stores from open, each case on a store of
// its own.
func Run(t *testing.T, open Open) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, context.Background(), open(t))
		})
	}
}

type testCase struct {
	name string
	run  func(t *testing.T, ctx context.Context, p store.Port)
}

var cases = []testCase{
	{"fabrics/round-trip", fabricsRoundTrip},
	{"fabrics/not-found", fabricsNotFound},
	{"fabrics/counter-does-not-reuse", fabricsCounterDoesNotReuse},
	{"fabrics/explicit-index", fabricsExplicitIndex},
	{"fabrics/exhausted-and-wrap", fabricsExhaustedAndWrap},
	{"fabrics/duplicate-root-and-id", fabricsDuplicate},
	{"identity", identityCase},
	{"acl", aclCase},
	{"group-keys", groupKeysCase},
	{"group-keys/ordering", groupKeysOrdering},
	{"groups", groupsCase},
	{"resumption", resumptionCase},
	{"server-subscriptions", serverSubscriptionsCase},
	{"settings", settingsCase},
	{"ota", otaCase},
	{"thermostat-presets", presetsCase},
	{"door-locks", doorLocksCase},
	{"unknown-fabric-refused", unknownFabricRefused},
	{"constraints", constraintsCase},
	{"fabric-cascade", fabricCascade},
	{"copies", copiesCase},
	{"concurrent", concurrentCase},
}

// fabric returns a distinct fabric record; the index is left to the store.
func fabric(n byte) store.FabricRecord {
	key := make([]byte, 65)
	key[0] = 0x04
	for i := 1; i < len(key); i++ {
		key[i] = n + byte(i)
	}
	return store.FabricRecord{
		FabricID:      0x1000 + uint64(n),
		NodeID:        0x2000 + uint64(n),
		RootPublicKey: key,
		RootCert:      []byte{0x15, n, 0x18},
		VendorID:      0xFFF1,
		Label:         fmt.Sprintf("fabric %d", n),
		CompressedID:  [8]byte{1, 2, 3, 4, 5, 6, 7, n},
	}
}

func addFabric(t *testing.T, ctx context.Context, p store.Port, n byte) uint8 {
	t.Helper()
	idx, err := p.AddFabric(ctx, fabric(n))
	if err != nil {
		t.Fatalf("AddFabric(%d): %v", n, err)
	}
	return idx
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %#v\nwant %#v", what, got, want)
	}
}

func fabricsRoundTrip(t *testing.T, ctx context.Context, p store.Port) {
	if got, err := p.ListFabrics(ctx); err != nil || len(got) != 0 {
		t.Fatalf("ListFabrics on an empty store = %v, %v", got, err)
	}
	if idx := addFabric(t, ctx, p, 1); idx != 1 {
		t.Fatalf("first fabric index = %d, want 1", idx)
	}
	if idx := addFabric(t, ctx, p, 2); idx != 2 {
		t.Fatalf("second fabric index = %d, want 2", idx)
	}
	want := fabric(1)
	want.FabricIndex = 1
	got, err := p.GetFabric(ctx, 1)
	must(t, err)
	wantEqual(t, "GetFabric(1)", got, want)

	must(t, p.UpdateFabricLabel(ctx, 1, "renamed"))
	must(t, p.UpdateFabricVendorID(ctx, 1, 0x1234))
	must(t, p.UpdateFabricNodeID(ctx, 1, 0xABCD))
	want.Label, want.VendorID, want.NodeID = "renamed", 0x1234, 0xABCD
	got, err = p.GetFabric(ctx, 1)
	must(t, err)
	wantEqual(t, "GetFabric(1) after the updates", got, want)

	list, err := p.ListFabrics(ctx)
	must(t, err)
	if len(list) != 2 || list[0].FabricIndex != 1 || list[1].FabricIndex != 2 {
		t.Fatalf("ListFabrics = %+v, want indexes 1, 2", list)
	}
	wantEqual(t, "ListFabrics()[0]", list[0], want)

	must(t, p.RemoveFabric(ctx, 1))
	list, err = p.ListFabrics(ctx)
	must(t, err)
	if len(list) != 1 || list[0].FabricIndex != 2 {
		t.Fatalf("ListFabrics after RemoveFabric(1) = %+v", list)
	}
}

func fabricsNotFound(t *testing.T, ctx context.Context, p store.Port) {
	for name, err := range map[string]error{
		"GetFabric":            func() error { _, err := p.GetFabric(ctx, 7); return err }(),
		"UpdateFabricLabel":    p.UpdateFabricLabel(ctx, 7, "x"),
		"UpdateFabricVendorID": p.UpdateFabricVendorID(ctx, 7, 1),
		"UpdateFabricNodeID":   p.UpdateFabricNodeID(ctx, 7, 1),
		"RemoveFabric":         p.RemoveFabric(ctx, 7),
	} {
		if !errors.Is(err, store.ErrFabricNotFound) {
			t.Errorf("%s on a missing fabric = %v, want ErrFabricNotFound", name, err)
		}
	}
}

// fabricsCounterDoesNotReuse is matter.js FabricManager #nextFabricIndex
// (FabricManager.ts:178-179): a removed index is not handed out again until
// the counter wraps.
func fabricsCounterDoesNotReuse(t *testing.T, ctx context.Context, p store.Port) {
	addFabric(t, ctx, p, 1)
	second := addFabric(t, ctx, p, 2)
	must(t, p.RemoveFabric(ctx, second))
	if idx := addFabric(t, ctx, p, 3); idx != 3 {
		t.Fatalf("index after removing %d = %d, want 3", second, idx)
	}
}

func fabricsExplicitIndex(t *testing.T, ctx context.Context, p store.Port) {
	rec := fabric(1)
	rec.FabricIndex = 9
	idx, err := p.AddFabric(ctx, rec)
	if err != nil || idx != 9 {
		t.Fatalf("AddFabric(index 9) = %d, %v", idx, err)
	}
	rec = fabric(2)
	rec.FabricIndex = 9
	if _, err := p.AddFabric(ctx, rec); err == nil {
		t.Fatal("AddFabric on a taken index succeeded")
	}
	rec.FabricIndex = 255
	if _, err := p.AddFabric(ctx, rec); err == nil {
		t.Fatal("AddFabric(index 255) succeeded")
	}
	// An explicit index does not move the counter.
	if idx := addFabric(t, ctx, p, 3); idx != 1 {
		t.Fatalf("auto index after an explicit 9 = %d, want 1", idx)
	}
}

func fabricsExhaustedAndWrap(t *testing.T, ctx context.Context, p store.Port) {
	for i := 1; i <= 254; i++ {
		if idx := addFabric(t, ctx, p, byte(i)); int(idx) != i {
			t.Fatalf("fabric %d took index %d", i, idx)
		}
	}
	if _, err := p.AddFabric(ctx, fabric(0)); !errors.Is(err, store.ErrFabricExhausted) {
		t.Fatalf("AddFabric on a full table = %v, want ErrFabricExhausted", err)
	}
	must(t, p.RemoveFabric(ctx, 5))
	if idx := addFabric(t, ctx, p, 0); idx != 5 {
		t.Fatalf("index after the wrap = %d, want the free 5", idx)
	}
}

func fabricsDuplicate(t *testing.T, ctx context.Context, p store.Port) {
	addFabric(t, ctx, p, 1)
	if _, err := p.AddFabric(ctx, fabric(1)); err == nil {
		t.Fatal("AddFabric of the same FabricID under the same root succeeded")
	}
}

func identityCase(t *testing.T, ctx context.Context, p store.Port) {
	idx := addFabric(t, ctx, p, 1)
	if _, err := p.GetIdentity(ctx, idx); !errors.Is(err, store.ErrIdentityNotFound) {
		t.Fatalf("GetIdentity before an upsert = %v, want ErrIdentityNotFound", err)
	}
	rec := store.IdentityRecord{FabricIndex: idx, NOC: []byte{1, 2}, PrivateKey: bytes.Repeat([]byte{3}, 32), IPK: bytes.Repeat([]byte{4}, 16)}
	must(t, p.UpsertIdentity(ctx, rec))
	got, err := p.GetIdentity(ctx, idx)
	must(t, err)
	wantEqual(t, "GetIdentity (no ICAC)", got, rec)
	if got.ICAC != nil {
		t.Fatalf("ICAC = %#v, want nil", got.ICAC)
	}
	rec.ICAC = []byte{9, 9}
	rec.NOC = []byte{5}
	must(t, p.UpsertIdentity(ctx, rec))
	got, err = p.GetIdentity(ctx, idx)
	must(t, err)
	wantEqual(t, "GetIdentity after the second upsert", got, rec)
}

func u32(v uint32) *uint32 { return &v }
func u16(v uint16) *uint16 { return &v }

func aclCase(t *testing.T, ctx context.Context, p store.Port) {
	one := addFabric(t, ctx, p, 1)
	two := addFabric(t, ctx, p, 2)
	entries := []store.ACLEntry{
		{Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0x1122}, Position: 40},
		{
			Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup, Subjects: []uint64{1, 2},
			Targets: []store.ACLTarget{{Cluster: u32(6)}, {Endpoint: u16(3), DeviceType: u32(0x100)}},
		},
	}
	must(t, p.ReplaceACL(ctx, one, entries))
	must(t, p.ReplaceACL(ctx, two, entries[:1]))
	got, err := p.ListACL(ctx, one)
	must(t, err)
	if len(got) != 2 {
		t.Fatalf("ListACL = %+v, want two entries", got)
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("two entries share ID %d", got[0].ID)
	}
	for i := range got {
		want := entries[i]
		want.ID, want.FabricIndex, want.Position = got[i].ID, one, uint16(i) //nolint:gosec // i < 2
		wantEqual(t, fmt.Sprintf("ListACL()[%d]", i), got[i], want)
	}
	must(t, p.ReplaceACL(ctx, one, entries[1:]))
	got, err = p.ListACL(ctx, one)
	must(t, err)
	if len(got) != 1 || got[0].Privilege != store.PrivilegeOperate || got[0].Position != 0 {
		t.Fatalf("ListACL after the second replace = %+v", got)
	}
	must(t, p.ReplaceACL(ctx, one, nil))
	if got, err = p.ListACL(ctx, one); err != nil || len(got) != 0 {
		t.Fatalf("ListACL after an empty replace = %+v, %v", got, err)
	}
	if got, err = p.ListACL(ctx, two); err != nil || len(got) != 1 {
		t.Fatalf("the other fabric's ACL = %+v, %v", got, err)
	}
	if got, err = p.ListACL(ctx, 99); err != nil || len(got) != 0 {
		t.Fatalf("ListACL of an unknown fabric = %+v, %v", got, err)
	}
}

func keySet(fabric uint8, id uint16) store.GroupKeySet {
	return store.GroupKeySet{
		FabricIndex: fabric, GroupKeySetID: id, SecurityPolicy: store.SecurityPolicyTrustFirst,
		EpochKey0: bytes.Repeat([]byte{byte(id)}, 16), EpochStart0: 100 + uint64(id),
	}
}

func groupKeysCase(t *testing.T, ctx context.Context, p store.Port) {
	f := addFabric(t, ctx, p, 1)
	if _, err := p.GetGroupKeySet(ctx, f, 1); !errors.Is(err, store.ErrGroupKeySetNotFound) {
		t.Fatalf("GetGroupKeySet on a missing set = %v, want ErrGroupKeySetNotFound", err)
	}
	full := keySet(f, 1)
	full.SecurityPolicy = store.SecurityPolicyCacheAndSync
	full.EpochKey1, full.EpochStart1 = bytes.Repeat([]byte{7}, 16), 200
	full.EpochKey2, full.EpochStart2 = bytes.Repeat([]byte{8}, 16), 300
	must(t, p.UpsertGroupKeySet(ctx, full))
	got, err := p.GetGroupKeySet(ctx, f, 1)
	must(t, err)
	wantEqual(t, "GetGroupKeySet(1)", got, full)

	// An epoch start without its key is not kept.
	partial := keySet(f, 2)
	partial.EpochStart1 = 999
	must(t, p.UpsertGroupKeySet(ctx, partial))
	got, err = p.GetGroupKeySet(ctx, f, 2)
	must(t, err)
	partial.EpochStart1 = 0
	wantEqual(t, "GetGroupKeySet(2)", got, partial)

	// Upsert replaces.
	full.EpochKey2, full.EpochStart2 = nil, 0
	must(t, p.UpsertGroupKeySet(ctx, full))
	got, err = p.GetGroupKeySet(ctx, f, 1)
	must(t, err)
	wantEqual(t, "GetGroupKeySet(1) after the second upsert", got, full)

	// The key map; a map entry may name a set that does not exist yet.
	for _, m := range []store.GroupKeyMapping{
		{FabricIndex: f, GroupID: 0x30, GroupKeySetID: 2},
		{FabricIndex: f, GroupID: 0x10, GroupKeySetID: 1},
		{FabricIndex: f, GroupID: 0x20, GroupKeySetID: 77},
	} {
		must(t, p.SetGroupKeyMapping(ctx, m))
	}
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 0x20, GroupKeySetID: 1}))
	maps, err := p.ListGroupKeyMappings(ctx, f)
	must(t, err)
	wantEqual(t, "ListGroupKeyMappings", maps, []store.GroupKeyMapping{
		{FabricIndex: f, GroupID: 0x10, GroupKeySetID: 1},
		{FabricIndex: f, GroupID: 0x20, GroupKeySetID: 1},
		{FabricIndex: f, GroupID: 0x30, GroupKeySetID: 2},
	})

	// Removing a set takes the map entries naming it along, and only those.
	must(t, p.RemoveGroupKeySet(ctx, f, 1))
	if _, err := p.GetGroupKeySet(ctx, f, 1); !errors.Is(err, store.ErrGroupKeySetNotFound) {
		t.Fatalf("GetGroupKeySet after RemoveGroupKeySet = %v", err)
	}
	maps, err = p.ListGroupKeyMappings(ctx, f)
	must(t, err)
	wantEqual(t, "ListGroupKeyMappings after RemoveGroupKeySet(1)", maps, []store.GroupKeyMapping{{FabricIndex: f, GroupID: 0x30, GroupKeySetID: 2}})
	must(t, p.RemoveGroupKeySet(ctx, f, 55)) // a missing set is no error

	must(t, p.RemoveGroupKeyMapping(ctx, f, 0x30))
	must(t, p.RemoveGroupKeyMapping(ctx, f, 0x30))
	if maps, err = p.ListGroupKeyMappings(ctx, f); err != nil || len(maps) != 0 {
		t.Fatalf("ListGroupKeyMappings after RemoveGroupKeyMapping = %+v, %v", maps, err)
	}

	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 1, GroupKeySetID: 2}))
	must(t, p.RemoveGroupKeysByFabric(ctx, f))
	sets, err := p.ListGroupKeySets(ctx, f)
	must(t, err)
	maps, err = p.ListGroupKeyMappings(ctx, f)
	must(t, err)
	if len(sets) != 0 || len(maps) != 0 {
		t.Fatalf("after RemoveGroupKeysByFabric: sets %+v, maps %+v", sets, maps)
	}
}

// groupKeysOrdering is the order ListGroupKeySets promises: by
// GroupKeySetID ascending, whatever the order of the writes.
func groupKeysOrdering(t *testing.T, ctx context.Context, p store.Port) {
	f := addFabric(t, ctx, p, 1)
	other := addFabric(t, ctx, p, 2)
	for _, id := range []uint16{3, 0, 0xFFFF, 1} {
		must(t, p.UpsertGroupKeySet(ctx, keySet(f, id)))
	}
	must(t, p.UpsertGroupKeySet(ctx, keySet(other, 2)))
	sets, err := p.ListGroupKeySets(ctx, f)
	must(t, err)
	var ids []uint16
	for _, s := range sets {
		if s.FabricIndex != f {
			t.Fatalf("ListGroupKeySets(%d) returned fabric %d's set", f, s.FabricIndex)
		}
		ids = append(ids, s.GroupKeySetID)
	}
	wantEqual(t, "ListGroupKeySets order", ids, []uint16{0, 1, 3, 0xFFFF})
}

func groupsCase(t *testing.T, ctx context.Context, p store.Port) {
	f := addFabric(t, ctx, p, 1)
	if err := p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 1}); err == nil {
		t.Fatal("UpsertGroupTableEntry without endpoints succeeded")
	}
	must(t, p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 9, GroupName: "nine", Endpoints: []uint16{3, 2}}))
	must(t, p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 4, GroupName: "four", Endpoints: []uint16{1}}))
	must(t, p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 4, GroupName: "vier", Endpoints: []uint16{1, 5}}))
	table, err := p.ListGroupTable(ctx, f)
	must(t, err)
	wantEqual(t, "ListGroupTable", table, []store.GroupTableEntry{
		{FabricIndex: f, GroupID: 4, GroupName: "vier", Endpoints: []uint16{1, 5}},
		{FabricIndex: f, GroupID: 9, GroupName: "nine", Endpoints: []uint16{3, 2}},
	})
	must(t, p.RemoveGroupTableEntry(ctx, f, 9))
	must(t, p.RemoveGroupTableEntry(ctx, f, 9))
	if table, err = p.ListGroupTable(ctx, f); err != nil || len(table) != 1 {
		t.Fatalf("ListGroupTable after the remove = %+v, %v", table, err)
	}

	must(t, p.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: f, GroupID: 7, McastAddrPolicy: 1, HasAuxiliaryACL: true}))
	must(t, p.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: f, GroupID: 2}))
	must(t, p.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: f, GroupID: 7, McastAddrPolicy: 0, HasAuxiliaryACL: false}))
	gc, err := p.ListGroupcastGroups(ctx, f)
	must(t, err)
	wantEqual(t, "ListGroupcastGroups", gc, []store.GroupcastGroup{{FabricIndex: f, GroupID: 2}, {FabricIndex: f, GroupID: 7}})
	must(t, p.RemoveGroupcastGroup(ctx, f, 2))
	if gc, err = p.ListGroupcastGroups(ctx, f); err != nil || len(gc) != 1 || gc[0].GroupID != 7 {
		t.Fatalf("ListGroupcastGroups after the remove = %+v, %v", gc, err)
	}
}

func resumptionRec(fabric uint8, peer uint64, id byte) store.ResumptionRecord {
	return store.ResumptionRecord{
		FabricIndex: fabric, PeerNodeID: peer,
		ResumptionID: bytes.Repeat([]byte{id}, 16), SharedSecret: bytes.Repeat([]byte{id + 1}, 32),
	}
}

func resumptionCase(t *testing.T, ctx context.Context, p store.Port) {
	f := addFabric(t, ctx, p, 1)
	if _, err := p.GetResumptionByPeer(ctx, f, 5); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("GetResumptionByPeer on a miss = %v, want ErrResumptionNotFound", err)
	}
	if _, err := p.GetResumptionByID(ctx, bytes.Repeat([]byte{1}, 16)); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("GetResumptionByID on a miss = %v, want ErrResumptionNotFound", err)
	}
	if _, err := p.GetResumptionByID(ctx, []byte{1}); err == nil || errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("GetResumptionByID(1 byte) = %v, want a length error", err)
	}
	bad := resumptionRec(f, 5, 1)
	bad.ResumptionID = bad.ResumptionID[:15]
	if err := p.UpsertResumption(ctx, bad); err == nil {
		t.Fatal("UpsertResumption with a 15-byte id succeeded")
	}
	bad = resumptionRec(f, 5, 1)
	bad.SharedSecret = bad.SharedSecret[:31]
	if err := p.UpsertResumption(ctx, bad); err == nil {
		t.Fatal("UpsertResumption with a 31-byte secret succeeded")
	}

	rec := resumptionRec(f, 5, 1)
	must(t, p.UpsertResumption(ctx, rec))
	got, err := p.GetResumptionByPeer(ctx, f, 5)
	must(t, err)
	if len(got.CASEAuthTags) != 0 {
		t.Fatalf("CASEAuthTags = %v, want none", got.CASEAuthTags)
	}
	got.CASEAuthTags = nil
	wantEqual(t, "GetResumptionByPeer", got, rec)

	rec2 := resumptionRec(f, 5, 3)
	rec2.CASEAuthTags = []uint32{0x00010001, 0x00020001}
	must(t, p.UpsertResumption(ctx, rec2))
	if _, err := p.GetResumptionByID(ctx, rec.ResumptionID); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("the replaced resumption id still resolves: %v", err)
	}
	got, err = p.GetResumptionByID(ctx, rec2.ResumptionID)
	must(t, err)
	wantEqual(t, "GetResumptionByID", got, rec2)

	must(t, p.UpsertResumption(ctx, resumptionRec(f, 6, 5)))
	must(t, p.RemoveResumption(ctx, f, 5))
	must(t, p.RemoveResumption(ctx, f, 5))
	if _, err := p.GetResumptionByPeer(ctx, f, 5); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("GetResumptionByPeer after RemoveResumption = %v", err)
	}
	must(t, p.RemoveResumptionsByFabric(ctx, f))
	if _, err := p.GetResumptionByPeer(ctx, f, 6); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("GetResumptionByPeer after RemoveResumptionsByFabric = %v", err)
	}
}

// serverSubscriptionsCase checks the load order the bridge restores
// subscriptions in (ADR 0008): the least recently saved first. Rows saved
// within one tick of a store's clock are ordered by subscription id, so the
// case saves them in id order.
func serverSubscriptionsCase(t *testing.T, ctx context.Context, p store.Port) {
	if got, err := p.LoadServerSubscriptions(ctx); err != nil || len(got) != 0 {
		t.Fatalf("LoadServerSubscriptions on an empty store = %v, %v", got, err)
	}
	must(t, p.SaveServerSubscription(ctx, 1, 1, 0x10, []byte("one")))
	must(t, p.SaveServerSubscription(ctx, 2, 2, 0x20, []byte("two")))
	must(t, p.SaveServerSubscription(ctx, 3, 1, 0x30, []byte("three")))
	must(t, p.SaveServerSubscription(ctx, 3, 1, 0x30, []byte("three'")))
	got, err := p.LoadServerSubscriptions(ctx)
	must(t, err)
	wantEqual(t, "LoadServerSubscriptions", got, [][]byte{[]byte("one"), []byte("two"), []byte("three'")})

	must(t, p.DeleteServerSubscription(ctx, 2))
	must(t, p.DeleteServerSubscription(ctx, 2))
	must(t, p.SaveServerSubscription(ctx, 4, 2, 0x40, []byte("four")))
	must(t, p.DeleteServerSubscriptionsByFabric(ctx, 1))
	got, err = p.LoadServerSubscriptions(ctx)
	must(t, err)
	wantEqual(t, "LoadServerSubscriptions after the deletes", got, [][]byte{[]byte("four")})
	must(t, p.ClearServerSubscriptions(ctx))
	if got, err = p.LoadServerSubscriptions(ctx); err != nil || len(got) != 0 {
		t.Fatalf("LoadServerSubscriptions after Clear = %v, %v", got, err)
	}
}

func settingsCase(t *testing.T, ctx context.Context, p store.Port) {
	if v, ok, err := p.GetSetting(ctx, store.SettingNodeLabel); err != nil || ok || v != "" {
		t.Fatalf("GetSetting on a missing key = %q, %v, %v", v, ok, err)
	}
	must(t, p.SetSetting(ctx, store.SettingNodeLabel, "kitchen"))
	must(t, p.SetSetting(ctx, store.SettingLocation, ""))
	must(t, p.SetSetting(ctx, store.SettingNodeLabel, "hall"))
	if v, ok, err := p.GetSetting(ctx, store.SettingNodeLabel); err != nil || !ok || v != "hall" {
		t.Fatalf("GetSetting = %q, %v, %v; want hall", v, ok, err)
	}
	if v, ok, err := p.GetSetting(ctx, store.SettingLocation); err != nil || !ok || v != "" {
		t.Fatalf("GetSetting of an empty value = %q, %v, %v; want it present", v, ok, err)
	}

	if v, ok, err := p.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); err != nil || ok || v != 0 {
		t.Fatalf("GetMetadataCounter on a missing key = %d, %v, %v", v, ok, err)
	}
	must(t, p.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 1<<40))
	if v, ok, err := p.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); err != nil || !ok || v != 1<<40 {
		t.Fatalf("GetMetadataCounter = %d, %v, %v; want 1<<40", v, ok, err)
	}
	must(t, p.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 1<<62))
	if err := p.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 1<<62+1); err == nil {
		t.Fatal("SetMetadataCounter above 1<<62 succeeded")
	}
	if v, _, err := p.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); err != nil || v != 1<<62 {
		t.Fatalf("a refused counter write changed the counter to %d (%v)", v, err)
	}
}

func otaCase(t *testing.T, ctx context.Context, p store.Port) {
	one := addFabric(t, ctx, p, 1)
	two := addFabric(t, ctx, p, 2)
	if got, err := p.ListDefaultOTAProviders(ctx); err != nil || len(got) != 0 {
		t.Fatalf("ListDefaultOTAProviders on an empty store = %v, %v", got, err)
	}
	must(t, p.ReplaceDefaultOTAProviders(ctx, two, []store.OTAProviderLocation{{ProviderNodeID: 0x22, Endpoint: 0}}))
	must(t, p.ReplaceDefaultOTAProviders(ctx, one, []store.OTAProviderLocation{
		{FabricIndex: 77, ProviderNodeID: 0x12, Endpoint: 2},
		{ProviderNodeID: 0x11, Endpoint: 1},
	}))
	got, err := p.ListDefaultOTAProviders(ctx)
	must(t, err)
	wantEqual(t, "ListDefaultOTAProviders", got, []store.OTAProviderLocation{
		{FabricIndex: one, ProviderNodeID: 0x12, Endpoint: 2},
		{FabricIndex: one, ProviderNodeID: 0x11, Endpoint: 1},
		{FabricIndex: two, ProviderNodeID: 0x22, Endpoint: 0},
	})
	must(t, p.ReplaceDefaultOTAProviders(ctx, one, nil))
	got, err = p.ListDefaultOTAProviders(ctx)
	must(t, err)
	wantEqual(t, "ListDefaultOTAProviders after clearing fabric 1", got, []store.OTAProviderLocation{{FabricIndex: two, ProviderNodeID: 0x22}})

	if state, ok, err := p.LoadOTARequestorState(ctx); err != nil || ok || state != nil {
		t.Fatalf("LoadOTARequestorState before a save = %v, %v, %v", state, ok, err)
	}
	must(t, p.SaveOTARequestorState(ctx, []byte{1, 2, 3}))
	state, ok, err := p.LoadOTARequestorState(ctx)
	if err != nil || !ok || !bytes.Equal(state, []byte{1, 2, 3}) {
		t.Fatalf("LoadOTARequestorState = %v, %v, %v", state, ok, err)
	}
}

func presetsCase(t *testing.T, ctx context.Context, p store.Port) {
	if pr, act, ok, err := p.LoadThermostatPresets(ctx, "t1"); err != nil || ok || pr != nil || act != nil {
		t.Fatalf("LoadThermostatPresets before a save = %v, %v, %v, %v", pr, act, ok, err)
	}
	must(t, p.SaveThermostatPresets(ctx, "t1", []byte{0x15, 0x18}, []byte{0xAA}))
	must(t, p.SaveThermostatPresets(ctx, "t2", nil, nil))
	pr, act, ok, err := p.LoadThermostatPresets(ctx, "t1")
	if err != nil || !ok || !bytes.Equal(pr, []byte{0x15, 0x18}) || !bytes.Equal(act, []byte{0xAA}) {
		t.Fatalf("LoadThermostatPresets(t1) = %v, %v, %v, %v", pr, act, ok, err)
	}
	pr, act, ok, err = p.LoadThermostatPresets(ctx, "t2")
	if err != nil || !ok || pr == nil || len(pr) != 0 || act != nil {
		t.Fatalf("LoadThermostatPresets(t2) = %#v, %#v, %v, %v; want empty presets and no handle", pr, act, ok, err)
	}
	must(t, p.SaveThermostatPresets(ctx, "t1", []byte{0x17}, nil))
	pr, act, _, err = p.LoadThermostatPresets(ctx, "t1")
	if err != nil || !bytes.Equal(pr, []byte{0x17}) || act != nil {
		t.Fatalf("LoadThermostatPresets(t1) after the second save = %v, %v, %v", pr, act, err)
	}
	ps := store.NewThermostatPresetStore(p, "t1")
	must(t, ps.SavePresets(ctx, []byte{0x18}, []byte{1}))
	if pr, act, ok, err = ps.LoadPresets(ctx); err != nil || !ok || !bytes.Equal(pr, []byte{0x18}) || !bytes.Equal(act, []byte{1}) {
		t.Fatalf("ThermostatPresetStore.LoadPresets = %v, %v, %v, %v", pr, act, ok, err)
	}
}

func doorLocksCase(t *testing.T, ctx context.Context, p store.Port) {
	if auth, saved, err := p.LoadDoorLockAuth(ctx, "lock"); err != nil || saved || !reflect.DeepEqual(auth, store.DoorLockAuth{}) {
		t.Fatalf("LoadDoorLockAuth before a save = %+v, %v, %v", auth, saved, err)
	}
	expires := int64(1_700_000_000_000)
	auth := store.DoorLockAuth{
		Users: []store.DoorLockUser{
			{
				UserIndex: 2, UserName: "b", UserStatus: 1, UserType: 0, Credentials: []store.DoorLockCredentialRef{{Type: 1, Index: 1}},
				CreatorFabricIndex: 1, LastModifiedFabricIndex: 2, ExpiringUserExpiresAt: &expires,
			},
			{UserIndex: 1, UserName: "a", UserUniqueID: u32(42), UserStatus: 1},
		},
		Credentials: []store.DoorLockCredential{
			{Type: 2, Index: 1, Data: []byte{9}},
			{Type: 1, Index: 3, Data: []byte{8}},
			{Type: 1, Index: 1, Data: []byte{7}, CreatorFabricIndex: 1, LastModifiedFabricIndex: 1},
		},
		WeekDaySchedules: []store.DoorLockWeekDaySchedule{
			{WeekDayIndex: 2, UserIndex: 1, DaysMask: 3, StartHour: 8, EndHour: 17},
			{WeekDayIndex: 1, UserIndex: 2, DaysMask: 1},
			{WeekDayIndex: 1, UserIndex: 1, DaysMask: 2, StartMinute: 30, EndMinute: 45},
		},
		YearDaySchedules: []store.DoorLockYearDaySchedule{
			{YearDayIndex: 2, UserIndex: 1, LocalStartTime: 10, LocalEndTime: 20},
			{YearDayIndex: 1, UserIndex: 1, LocalStartTime: 1, LocalEndTime: 2},
		},
		HolidaySchedules: []store.DoorLockHolidaySchedule{
			{HolidayIndex: 3, LocalStartTime: 5, LocalEndTime: 6, OperatingMode: 1},
			{HolidayIndex: 1, LocalStartTime: 3, LocalEndTime: 4},
		},
	}
	must(t, p.SaveDoorLockAuth(ctx, "lock", auth))
	got, saved, err := p.LoadDoorLockAuth(ctx, "lock")
	if err != nil || !saved {
		t.Fatalf("LoadDoorLockAuth = %v, %v", saved, err)
	}
	want := store.DoorLockAuth{
		Users: []store.DoorLockUser{
			{UserIndex: 1, UserName: "a", UserUniqueID: u32(42), UserStatus: 1, Credentials: []store.DoorLockCredentialRef{}},
			auth.Users[0],
		},
		Credentials:      []store.DoorLockCredential{auth.Credentials[2], auth.Credentials[1], auth.Credentials[0]},
		WeekDaySchedules: []store.DoorLockWeekDaySchedule{auth.WeekDaySchedules[2], auth.WeekDaySchedules[0], auth.WeekDaySchedules[1]},
		YearDaySchedules: []store.DoorLockYearDaySchedule{auth.YearDaySchedules[1], auth.YearDaySchedules[0]},
		HolidaySchedules: []store.DoorLockHolidaySchedule{auth.HolidaySchedules[1], auth.HolidaySchedules[0]},
	}
	wantEqual(t, "LoadDoorLockAuth", got, want)

	if _, saved, _ := p.LoadDoorLockAuth(ctx, "other"); saved {
		t.Fatal("another endpoint reads as saved")
	}
	dup := store.DoorLockAuth{Users: []store.DoorLockUser{{UserIndex: 1}, {UserIndex: 1}}}
	if err := p.SaveDoorLockAuth(ctx, "lock", dup); err == nil {
		t.Fatal("SaveDoorLockAuth with a user index twice succeeded")
	}
	if got, _, err = p.LoadDoorLockAuth(ctx, "lock"); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("a refused save changed the records: %+v, %v", got, err)
	}
	must(t, p.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{}))
	got, saved, err = p.LoadDoorLockAuth(ctx, "lock")
	if err != nil || !saved || !reflect.DeepEqual(got, store.DoorLockAuth{}) {
		t.Fatalf("LoadDoorLockAuth after an empty save = %+v, %v, %v; want saved and empty", got, saved, err)
	}
}

// unknownFabricRefused is the foreign key of every fabric-owned row.
func unknownFabricRefused(t *testing.T, ctx context.Context, p store.Port) {
	const missing = 42
	for name, err := range map[string]error{
		"UpsertIdentity":             p.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: missing, NOC: []byte{1}, PrivateKey: []byte{2}, IPK: []byte{3}}),
		"ReplaceACL":                 p.ReplaceACL(ctx, missing, []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{1}}}),
		"UpsertGroupKeySet":          p.UpsertGroupKeySet(ctx, keySet(missing, 1)),
		"SetGroupKeyMapping":         p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: missing, GroupID: 1, GroupKeySetID: 1}),
		"UpsertGroupTableEntry":      p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: missing, GroupID: 1, Endpoints: []uint16{1}}),
		"UpsertGroupcastGroup":       p.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: missing, GroupID: 1}),
		"UpsertResumption":           p.UpsertResumption(ctx, resumptionRec(missing, 1, 1)),
		"ReplaceDefaultOTAProviders": p.ReplaceDefaultOTAProviders(ctx, missing, []store.OTAProviderLocation{{ProviderNodeID: 1}}),
	} {
		if err == nil {
			t.Errorf("%s for an unknown fabric succeeded", name)
		}
	}
	// Writes that add no row need no fabric.
	must(t, p.ReplaceACL(ctx, missing, nil))
	must(t, p.ReplaceDefaultOTAProviders(ctx, missing, nil))
}

// fabricCascade is the removal of a fabric: everything it owns goes, the
// other fabric's rows and the rows no fabric owns stay. matter.js clears the
// fabric's StorageContext (FabricManager.ts:262-271).
func fabricCascade(t *testing.T, ctx context.Context, p store.Port) {
	one := addFabric(t, ctx, p, 1)
	two := addFabric(t, ctx, p, 2)
	for _, f := range []uint8{one, two} {
		must(t, p.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: f, NOC: []byte{f}, PrivateKey: []byte{2}, IPK: []byte{3}}))
		must(t, p.ReplaceACL(ctx, f, []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{uint64(f)}}}))
		must(t, p.UpsertGroupKeySet(ctx, keySet(f, 1)))
		must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 1, GroupKeySetID: 1}))
		must(t, p.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 1, Endpoints: []uint16{1}}))
		must(t, p.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: f, GroupID: 1}))
		must(t, p.UpsertResumption(ctx, resumptionRec(f, 7, f)))
		must(t, p.ReplaceDefaultOTAProviders(ctx, f, []store.OTAProviderLocation{{ProviderNodeID: uint64(f)}}))
		must(t, p.SaveServerSubscription(ctx, uint32(f), f, 7, []byte{f}))
	}
	must(t, p.SaveThermostatPresets(ctx, "t", []byte{1}, nil))
	must(t, p.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{Users: []store.DoorLockUser{{UserIndex: 1, CreatorFabricIndex: one}}}))
	must(t, p.SetSetting(ctx, "k", "v"))

	must(t, p.RemoveFabric(ctx, one))

	count := func(f uint8) map[string]int {
		n := map[string]int{"identity": 0, "resumption": 0, "ota": 0}
		if _, err := p.GetIdentity(ctx, f); err == nil {
			n["identity"]++
		}
		acl, _ := p.ListACL(ctx, f)
		n["acl"] = len(acl)
		sets, _ := p.ListGroupKeySets(ctx, f)
		n["key sets"] = len(sets)
		maps, _ := p.ListGroupKeyMappings(ctx, f)
		n["key map"] = len(maps)
		table, _ := p.ListGroupTable(ctx, f)
		n["group table"] = len(table)
		gc, _ := p.ListGroupcastGroups(ctx, f)
		n["groupcast"] = len(gc)
		if _, err := p.GetResumptionByPeer(ctx, f, 7); err == nil {
			n["resumption"]++
		}
		ota, _ := p.ListDefaultOTAProviders(ctx)
		for _, o := range ota {
			if o.FabricIndex == f {
				n["ota"]++
			}
		}
		return n
	}
	all := map[string]int{"identity": 1, "acl": 1, "key sets": 1, "key map": 1, "group table": 1, "groupcast": 1, "resumption": 1, "ota": 1}
	none := map[string]int{"identity": 0, "acl": 0, "key sets": 0, "key map": 0, "group table": 0, "groupcast": 0, "resumption": 0, "ota": 0}
	wantEqual(t, "the removed fabric's rows", count(one), none)
	wantEqual(t, "the other fabric's rows", count(two), all)
	if _, err := p.GetResumptionByID(ctx, resumptionRec(one, 7, one).ResumptionID); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Errorf("the removed fabric's resumption id still resolves: %v", err)
	}

	subs, err := p.LoadServerSubscriptions(ctx)
	must(t, err)
	if len(subs) != 2 {
		t.Errorf("server subscriptions after RemoveFabric = %d, want both: the bridge deletes its own", len(subs))
	}
	if _, _, ok, _ := p.LoadThermostatPresets(ctx, "t"); !ok {
		t.Error("RemoveFabric removed the thermostat presets")
	}
	if auth, saved, _ := p.LoadDoorLockAuth(ctx, "lock"); !saved || len(auth.Users) != 1 {
		t.Error("RemoveFabric removed the door-lock records")
	}
	if _, ok, _ := p.GetSetting(ctx, "k"); !ok {
		t.Error("RemoveFabric removed a setting")
	}
}

// copiesCase: a caller that mutates what it wrote or what it read does not
// change the store.
func copiesCase(t *testing.T, ctx context.Context, p store.Port) {
	rec := fabric(1)
	idx, err := p.AddFabric(ctx, rec)
	must(t, err)
	rec.RootPublicKey[1] ^= 0xFF
	rec.RootCert[0] ^= 0xFF
	got, err := p.GetFabric(ctx, idx)
	must(t, err)
	if !bytes.Equal(got.RootPublicKey, fabric(1).RootPublicKey) || !bytes.Equal(got.RootCert, fabric(1).RootCert) {
		t.Fatal("mutating the written FabricRecord changed the store")
	}
	got.RootPublicKey[1] ^= 0xFF
	if again, _ := p.GetFabric(ctx, idx); !bytes.Equal(again.RootPublicKey, fabric(1).RootPublicKey) {
		t.Fatal("mutating a read FabricRecord changed the store")
	}

	entries := []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{1}, Targets: []store.ACLTarget{{Cluster: u32(6)}}}}
	must(t, p.ReplaceACL(ctx, idx, entries))
	entries[0].Subjects[0] = 99
	*entries[0].Targets[0].Cluster = 99
	acl, err := p.ListACL(ctx, idx)
	must(t, err)
	if acl[0].Subjects[0] != 1 || *acl[0].Targets[0].Cluster != 6 {
		t.Fatal("mutating the written ACL changed the store")
	}
	acl[0].Subjects[0] = 98
	*acl[0].Targets[0].Cluster = 98
	if again, _ := p.ListACL(ctx, idx); again[0].Subjects[0] != 1 || *again[0].Targets[0].Cluster != 6 {
		t.Fatal("mutating a read ACL changed the store")
	}

	e := store.GroupTableEntry{FabricIndex: idx, GroupID: 1, Endpoints: []uint16{1}}
	must(t, p.UpsertGroupTableEntry(ctx, e))
	e.Endpoints[0] = 9
	if table, _ := p.ListGroupTable(ctx, idx); table[0].Endpoints[0] != 1 {
		t.Fatal("mutating a written GroupTableEntry changed the store")
	}

	payload := []byte("sub")
	must(t, p.SaveServerSubscription(ctx, 1, idx, 1, payload))
	payload[0] = 'X'
	subs, err := p.LoadServerSubscriptions(ctx)
	must(t, err)
	if string(subs[0]) != "sub" {
		t.Fatal("mutating a written subscription payload changed the store")
	}
	subs[0][0] = 'Y'
	if again, _ := p.LoadServerSubscriptions(ctx); string(again[0]) != "sub" {
		t.Fatal("mutating a read subscription payload changed the store")
	}

	presets := []byte{1}
	must(t, p.SaveThermostatPresets(ctx, "t", presets, nil))
	presets[0] = 2
	if pr, _, _, _ := p.LoadThermostatPresets(ctx, "t"); pr[0] != 1 {
		t.Fatal("mutating written presets changed the store")
	}

	auth := store.DoorLockAuth{
		Users:       []store.DoorLockUser{{UserIndex: 1, UserUniqueID: u32(5), Credentials: []store.DoorLockCredentialRef{{Type: 1, Index: 1}}}},
		Credentials: []store.DoorLockCredential{{Type: 1, Index: 1, Data: []byte{7}}},
	}
	must(t, p.SaveDoorLockAuth(ctx, "lock", auth))
	*auth.Users[0].UserUniqueID = 6
	auth.Users[0].Credentials[0].Index = 2
	auth.Credentials[0].Data[0] = 8
	got2, _, err := p.LoadDoorLockAuth(ctx, "lock")
	must(t, err)
	if *got2.Users[0].UserUniqueID != 5 || got2.Users[0].Credentials[0].Index != 1 || got2.Credentials[0].Data[0] != 7 {
		t.Fatal("mutating written door-lock records changed the store")
	}
}

// concurrentCase runs writers and readers of several areas at once; under
// the race detector it is the check that the implementation synchronises.
func concurrentCase(t *testing.T, ctx context.Context, p store.Port) {
	fabrics := []uint8{addFabric(t, ctx, p, 1), addFabric(t, ctx, p, 2), addFabric(t, ctx, p, 3), addFabric(t, ctx, p, 4)}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i, f := range fabrics {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 10 {
				key := fmt.Sprintf("k%d", i)
				if err := p.SetSetting(ctx, key, fmt.Sprint(j)); err != nil {
					errs <- err
					return
				}
				if _, _, err := p.GetSetting(ctx, key); err != nil {
					errs <- err
					return
				}
				if err := p.ReplaceACL(ctx, f, []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{uint64(j)}}}); err != nil {
					errs <- err
					return
				}
				if _, err := p.ListACL(ctx, f); err != nil {
					errs <- err
					return
				}
				if err := p.SaveServerSubscription(ctx, uint32(i*100+j), f, 1, []byte{byte(j)}); err != nil { //nolint:gosec // small
					errs <- err
					return
				}
				if _, err := p.ListFabrics(ctx); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	subs, err := p.LoadServerSubscriptions(ctx)
	must(t, err)
	if len(subs) != 40 {
		t.Fatalf("LoadServerSubscriptions after the writers = %d rows, want 40", len(subs))
	}
	for _, f := range fabrics {
		acl, err := p.ListACL(ctx, f)
		must(t, err)
		if len(acl) != 1 || acl[0].Subjects[0] != 9 {
			t.Fatalf("fabric %d ACL after the writers = %+v", f, acl)
		}
	}
}

// constraintsCase is the rest of what the SQLite schema refuses: an ACL
// entry's privilege and auth mode outside their enums, one resumption id
// for two peers, and every door-lock record index used twice. A refused
// write changes nothing.
func constraintsCase(t *testing.T, ctx context.Context, p store.Port) {
	f := addFabric(t, ctx, p, 1)
	for _, e := range []store.ACLEntry{
		{Privilege: 0, AuthMode: store.AuthModeCASE},
		{Privilege: 6, AuthMode: store.AuthModeCASE},
		{Privilege: store.PrivilegeView, AuthMode: 0},
		{Privilege: store.PrivilegeView, AuthMode: 4},
	} {
		if err := p.ReplaceACL(ctx, f, []store.ACLEntry{e}); err == nil {
			t.Errorf("ReplaceACL with privilege %d, auth mode %d succeeded", e.Privilege, e.AuthMode)
		}
	}

	must(t, p.UpsertResumption(ctx, resumptionRec(f, 1, 1)))
	if err := p.UpsertResumption(ctx, resumptionRec(f, 2, 1)); err == nil {
		t.Error("UpsertResumption of another peer under the same resumption id succeeded")
	}
	if _, err := p.GetResumptionByPeer(ctx, f, 2); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Errorf("the refused resumption was stored: %v", err)
	}

	for name, auth := range map[string]store.DoorLockAuth{
		"credential": {Credentials: []store.DoorLockCredential{{Type: 1, Index: 1}, {Type: 1, Index: 1}}},
		"week day":   {WeekDaySchedules: []store.DoorLockWeekDaySchedule{{WeekDayIndex: 1, UserIndex: 1}, {WeekDayIndex: 1, UserIndex: 1}}},
		"year day":   {YearDaySchedules: []store.DoorLockYearDaySchedule{{YearDayIndex: 1, UserIndex: 1}, {YearDayIndex: 1, UserIndex: 1}}},
		"holiday":    {HolidaySchedules: []store.DoorLockHolidaySchedule{{HolidayIndex: 1}, {HolidayIndex: 1}}},
	} {
		if err := p.SaveDoorLockAuth(ctx, "lock", auth); err == nil {
			t.Errorf("SaveDoorLockAuth with a %s index twice succeeded", name)
		}
	}
	if _, saved, err := p.LoadDoorLockAuth(ctx, "lock"); err != nil || saved {
		t.Errorf("a refused SaveDoorLockAuth marked the lock saved (%v)", err)
	}
	// The same index for two users is two week-day schedules.
	must(t, p.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{WeekDaySchedules: []store.DoorLockWeekDaySchedule{{WeekDayIndex: 1, UserIndex: 1}, {WeekDayIndex: 1, UserIndex: 2}}}))
}
