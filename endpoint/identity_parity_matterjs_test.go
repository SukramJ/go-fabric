// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/store"
)

// TestEndpointNumberWrapParityMatterJS ports matter.js
// packages/node/test/node/ServerNodeTest.ts:1153-1176 at 862ad75f ("wraps
// new endpoint numbers to the lowest free number"): with the persisted next
// number at 0xFFFE, the next endpoint takes 0xFFFE, the one after it wraps
// to the lowest number no endpoint holds — matter.js
// ServerEndpointStores.assignNumber (#nextFreeNumber). matter.js's
// part numbers start at 1 on a node without an Aggregator; a Go bridged
// endpoint's at 2 (endpoint 1 is the Aggregator), so each number here is
// matter.js's plus one.
func TestEndpointNumberWrapParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "endpoints.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.New(db)
	add := func(key string) uint16 {
		t.Helper()
		id, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey(key), Scope: "s", DeviceType: 0x0100})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if first := add("first"); first != 2 {
		t.Fatalf("first %d, want 2", first)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_endpoint_allocation SET next_endpoint_id = ? WHERE id = 1`, 0xFFFE); err != nil {
		t.Fatal(err)
	}
	if again := add("first"); again != 2 {
		t.Fatalf("first after the restart %d, want its stored 2", again)
	}
	last := add("last")
	wrapped := add("wrapped")
	if err := s.RemoveEndpoint(ctx, endpoint.StringKey("wrapped")); err != nil {
		t.Fatal(err)
	}
	next := add("next")
	// matter.js walks on from the number it assigned last (next = wrapped
	// + 1); the store lifts its counter past the top number a row holds on
	// every allocation, so once wrapped it takes the lowest free number
	// again — the one "wrapped" freed (BD-Matter-EndpointNumberAfterWrap).
	if last != 0xFFFE || wrapped != 3 || next != 3 {
		t.Fatalf("last %#x wrapped %d next %d, want 0xfffe, 3 and 3", last, wrapped, next)
	}
}

// TestEndpointNumberAfterPresetParityMatterJS ports ServerNodeTest.ts:
// 1144-1151 at 862ad75f ("continues new endpoint numbers after the highest
// preset number"): a number placed by hand (matter.js's preset, a Go
// record with its EndpointID set) lifts the counter, and the next automatic
// number follows it (ServerEndpointStores.ts:156-158).
func TestEndpointNumberAfterPresetParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "endpoints.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.New(db)
	if id, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("preset"), Scope: "s", EndpointID: 10, DeviceType: 0x0100}); err != nil || id != 10 {
		t.Fatalf("preset %d, %v", id, err)
	}
	id, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("automatic"), Scope: "s", DeviceType: 0x0100})
	if err != nil || id != 11 {
		t.Fatalf("automatic %d (%v), want 11", id, err)
	}
}
