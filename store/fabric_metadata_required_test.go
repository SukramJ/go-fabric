// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/store"
)

// AddFabric requires the matter_metadata table: its first statement writes
// the counter row to take the write lock, so a database without the table
// refuses the insert rather than falling back to a scan, as its doc states
// (review II, R2-P7-sec-03).
func TestAddFabricRequiresTheMetadataTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	if _, err := db.ExecContext(ctx, `DROP TABLE matter_metadata`); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	if _, err := s.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 1, RootPublicKey: uncompressedP256Fixture(1), RootCert: []byte{1}, VendorID: 0xFFF1}); err == nil {
		t.Fatal("AddFabric without matter_metadata succeeded")
	}
	recs, err := s.ListFabrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("%d fabrics after the refused insert, want none", len(recs))
	}
}
