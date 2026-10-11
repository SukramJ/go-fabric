// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"reflect"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

// TestDefaultOTAProviders_FabricScoped: a replace touches only its own
// fabric's entries, keeps the written order, carries a 64-bit node id, and a
// removed fabric takes its entries along (the ACL table's pattern).
func TestDefaultOTAProviders_FabricScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	addTestFabric(t, s, 1, 1)
	addTestFabric(t, s, 2, 2)

	if got, err := s.ListDefaultOTAProviders(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}
	f1 := []store.OTAProviderLocation{
		{FabricIndex: 9, ProviderNodeID: 0xFFFF_FFFF_0000_0001, Endpoint: 0},
		{ProviderNodeID: 0x22, Endpoint: 7},
	}
	if err := s.ReplaceDefaultOTAProviders(ctx, 1, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDefaultOTAProviders(ctx, 2, []store.OTAProviderLocation{{ProviderNodeID: 0x33}}); err != nil {
		t.Fatal(err)
	}
	want := []store.OTAProviderLocation{
		{FabricIndex: 1, ProviderNodeID: 0xFFFF_FFFF_0000_0001, Endpoint: 0},
		{FabricIndex: 1, ProviderNodeID: 0x22, Endpoint: 7},
		{FabricIndex: 2, ProviderNodeID: 0x33},
	}
	got, err := s.ListDefaultOTAProviders(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("list %+v, %v; want %+v", got, err, want)
	}

	if err := s.ReplaceDefaultOTAProviders(ctx, 2, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListDefaultOTAProviders(ctx); len(got) != 2 {
		t.Fatalf("after clearing fabric 2: %+v", got)
	}
	if err := s.RemoveFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListDefaultOTAProviders(ctx); len(got) != 0 {
		t.Fatalf("after removing fabric 1: %+v", got)
	}
}

// TestOTARequestorState round-trips the requestor's opaque state.
func TestOTARequestorState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	if _, ok, err := s.LoadOTARequestorState(ctx); ok || err != nil {
		t.Fatalf("fresh store: ok=%v err=%v", ok, err)
	}
	if err := s.SaveOTARequestorState(ctx, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.LoadOTARequestorState(ctx)
	if err != nil || !ok || string(v) != `{"a":1}` {
		t.Fatalf("%q %v %v", v, ok, err)
	}
}
