// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"slices"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

func TestGroupTable_RoundTripReplaceRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	addTestFabric(t, s, 1, 1)
	addTestFabric(t, s, 2, 2)

	for _, e := range []store.GroupTableEntry{
		{FabricIndex: 1, GroupID: 0x0102, GroupName: "Hall", Endpoints: []uint16{4, 2}},
		{FabricIndex: 1, GroupID: 0x0101, GroupName: "Kitchen", Endpoints: []uint16{3}},
		{FabricIndex: 2, GroupID: 0x0101, GroupName: "Other fabric", Endpoints: []uint16{3}},
	} {
		if err := s.UpsertGroupTableEntry(ctx, e); err != nil {
			t.Fatalf("UpsertGroupTableEntry: %v", err)
		}
	}
	got, err := s.ListGroupTable(ctx, 1)
	if err != nil {
		t.Fatalf("ListGroupTable: %v", err)
	}
	if len(got) != 2 || got[0].GroupID != 0x0101 || got[1].GroupID != 0x0102 || !slices.Equal(got[1].Endpoints, []uint16{4, 2}) {
		t.Fatalf("fabric 1 table = %+v, want two rows ordered by group id, endpoint order kept", got)
	}

	// Replace in place.
	if err := s.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: 1, GroupID: 0x0101, GroupName: "Renamed", Endpoints: []uint16{3, 5}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListGroupTable(ctx, 1)
	if got[0].GroupName != "Renamed" || !slices.Equal(got[0].Endpoints, []uint16{3, 5}) {
		t.Fatalf("replaced row = %+v", got[0])
	}

	if err := s.RemoveGroupTableEntry(ctx, 1, 0x0102); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupTableEntry(ctx, 1, 0x7777); err != nil {
		t.Fatalf("removing a missing row must not fail: %v", err)
	}
	got, _ = s.ListGroupTable(ctx, 1)
	if len(got) != 1 {
		t.Fatalf("after remove: %+v", got)
	}

	// An entry without endpoints is refused.
	if err := s.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: 1, GroupID: 9}); err == nil {
		t.Fatal("an entry without endpoints was stored")
	}

	// Removing the fabric cascades.
	if err := s.RemoveFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListGroupTable(ctx, 1); len(got) != 0 {
		t.Fatalf("group table survived its fabric: %+v", got)
	}
	if got, _ := s.ListGroupTable(ctx, 2); len(got) != 1 {
		t.Fatalf("other fabric's table touched: %+v", got)
	}
}
