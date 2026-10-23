// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package memstore_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/store/storetest"
)

// TestMemstoreContract holds the in-memory store to the port's contract,
// the same table SQLite's Store runs.
func TestMemstoreContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Port { return memstore.New() })
}

// TestNextFabricIndexIsTheMetadataCounter: the counter lives where SQLite
// keeps it, a metadata counter starting at 1 (store/schema.sql).
func TestNextFabricIndexIsTheMetadataCounter(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	if v, ok, err := s.GetMetadataCounter(ctx, "next_fabric_index"); err != nil || !ok || v != 1 {
		t.Fatalf("fresh counter = %d, %v, %v; want 1", v, ok, err)
	}
	if _, err := s.AddFabric(ctx, store.FabricRecord{FabricID: 1, RootPublicKey: []byte{4}}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := s.GetMetadataCounter(ctx, "next_fabric_index"); v != 2 {
		t.Fatalf("counter after one fabric = %d, want 2", v)
	}
}
