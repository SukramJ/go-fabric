// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
)

// TestReusedRecordTakesTheNewScope is brief BV-1 §4's store fact: a source
// that moves from scope A to scope B keeps its endpoint number, and its
// record moves to B, so B's GC collects it once B is complete and the
// source is gone.
func TestReusedRecordTakesTheNewScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newFakeStore()
	a, err := endpoint.New(store, endpoint.Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	key := endpoint.StringKey("lamp")
	lamp := endpoint.Spec{StableKey: key, DeviceType: 0x010A, FriendlyName: "Lamp", Source: specSource{deviceType: 0x010A}}
	assemble := func(snaps ...endpoint.Snapshot) {
		t.Helper()
		if _, err := a.Assemble(ctx, snaps); err != nil {
			t.Fatal(err)
		}
	}

	assemble(endpoint.Snapshot{Scope: "A", Endpoints: []endpoint.Spec{lamp}})
	id := store.rows[key].EndpointID
	assemble(endpoint.Snapshot{Scope: "A"}, endpoint.Snapshot{Scope: "B", Endpoints: []endpoint.Spec{lamp}, ModelComplete: true})
	rec, ok := store.rows[key]
	if !ok || rec.EndpointID != id || rec.Scope != "B" {
		t.Fatalf("after the move: %+v (stored %v), want endpoint %d under scope B", rec, ok, id)
	}
	assemble(endpoint.Snapshot{Scope: "A"}, endpoint.Snapshot{Scope: "B", ModelComplete: true})
	if _, ok := store.rows[key]; ok {
		t.Error("removed from complete scope B, the record survived")
	}
}
