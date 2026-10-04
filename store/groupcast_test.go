// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

func TestGroupcastGroups_RoundTripAndCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	addTestFabric(t, s, 1, 1)
	addTestFabric(t, s, 2, 2)

	for _, g := range []store.GroupcastGroup{
		{FabricIndex: 1, GroupID: 7, McastAddrPolicy: 0},
		{FabricIndex: 1, GroupID: 3, McastAddrPolicy: 1, HasAuxiliaryACL: true},
		{FabricIndex: 2, GroupID: 3, McastAddrPolicy: 0},
	} {
		if err := s.UpsertGroupcastGroup(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	// An upsert replaces both properties.
	if err := s.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: 1, GroupID: 7, McastAddrPolicy: 1, HasAuxiliaryACL: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListGroupcastGroups(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.GroupcastGroup{
		{FabricIndex: 1, GroupID: 3, McastAddrPolicy: 1, HasAuxiliaryACL: true},
		{FabricIndex: 1, GroupID: 7, McastAddrPolicy: 1, HasAuxiliaryACL: true},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ListGroupcastGroups = %+v, want %+v", got, want)
	}
	if err := s.RemoveGroupcastGroup(ctx, 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupcastGroup(ctx, 1, 3); err != nil {
		t.Fatalf("removing a missing row: %v", err)
	}
	if got, _ := s.ListGroupcastGroups(ctx, 1); len(got) != 1 || got[0].GroupID != 7 {
		t.Fatalf("after removal: %+v", got)
	}
	if err := s.RemoveFabric(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListGroupcastGroups(ctx, 2); len(got) != 0 {
		t.Fatalf("fabric removal did not cascade: %+v", got)
	}
	if err := s.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: 9, GroupID: 1}); err == nil {
		t.Error("a row for an unknown fabric was accepted")
	}
}

func TestGroupcastGroups_ClosedDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := closedStore(t)
	if err := s.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: 1, GroupID: 1}); err == nil {
		t.Error("UpsertGroupcastGroup on closed DB: want error")
	}
	if err := s.RemoveGroupcastGroup(ctx, 1, 1); err == nil {
		t.Error("RemoveGroupcastGroup on closed DB: want error")
	}
	if _, err := s.ListGroupcastGroups(ctx, 1); err == nil {
		t.Error("ListGroupcastGroups on closed DB: want error")
	}
}
