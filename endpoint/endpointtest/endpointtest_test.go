// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpointtest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// recorder is a testing.TB that records failures instead of failing.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestAssertDeviceTypeConformance(t *testing.T) {
	t.Parallel()
	topology, err := endpointtest.NewEmptySnapshotter()(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The root and the Aggregator carry no servers: every mandatory one is
	// missing, starting with Base's Descriptor.
	r := &recorder{TB: t}
	found := endpointtest.AssertDeviceTypeConformance(r, topology)
	if len(found) == 0 || len(r.errs) != len(found) || !strings.HasPrefix(r.errs[0], "device type violation: endpoint 0: missing RootNode") {
		t.Fatalf("errors = %v for %d violations", r.errs, len(found))
	}

	// Tolerating every one of them passes; a stale tolerance fails.
	tolerate := make([]string, 0, len(found)+1)
	for i := range found {
		tolerate = append(tolerate, fmt.Sprintf("%d/%s", found[i].Endpoint, found[i].Key()))
	}
	r = &recorder{TB: t}
	endpointtest.AssertDeviceTypeConformance(r, topology, append(tolerate, "5/missing Nothing")...)
	if len(r.errs) != 1 || !strings.Contains(r.errs[0], `"5/missing Nothing" no longer occurs`) {
		t.Errorf("errors = %v", r.errs)
	}

	r = &recorder{TB: t}
	if got := endpointtest.AssertDeviceTypeConformance(r, &endpoint.Topology{}); len(got) != 0 || len(r.errs) != 0 {
		t.Errorf("an empty topology: %v / %v", got, r.errs)
	}
}

// TestFakeStoreErase: the fake's EraseEndpoints drops every row and the
// next assignment is 2 again, as the factory reset expects of a store.
func TestFakeStoreErase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := endpointtest.NewFakeStore()
	for _, k := range []string{"a", "b"} {
		if _, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey(k), Scope: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EraseEndpoints(ctx); err != nil {
		t.Fatal(err)
	}
	if recs, _ := s.ListEndpoints(ctx, ""); len(recs) != 0 {
		t.Fatalf("records after the erase: %v", recs)
	}
	if id, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("c"), Scope: "s"}); err != nil || id != 2 {
		t.Fatalf("first number after the erase %d (%v), want 2", id, err)
	}
}

// TestEveryFabricListsEveryIndex: the fabric source a test's Binding
// servers take lists the indexes 1 to 254, once each (review II,
// R2-P6-store-1010, decision 158).
func TestEveryFabricListsEveryIndex(t *testing.T) {
	t.Parallel()
	got, err := endpointtest.EveryFabric.ListFabrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 254 {
		t.Fatalf("%d fabrics, want 254", len(got))
	}
	for i, f := range got {
		if int(f.FabricIndex) != i+1 {
			t.Fatalf("entry %d is fabric %d, want %d", i, f.FabricIndex, i+1)
		}
	}
	if c := endpointtest.AssemblerConfig(); c.Fabrics != endpointtest.EveryFabric {
		t.Error("the assembler config does not carry EveryFabric")
	}
}

// TestFakeStoreNumbersAsThePortRequires: the fake keeps the number stored
// for a source when a record re-states it with 0 — before, it handed out a
// fresh one, which no real store does — lists ascending by number, and
// wraps past the top skipping the numbers still held (review II,
// R2-P6-test-0612).
func TestFakeStoreNumbersAsThePortRequires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := endpointtest.NewFakeStore()
	upsert := func(key string, id uint16) uint16 {
		t.Helper()
		got, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey(key), Scope: "s", EndpointID: id})
		if err != nil {
			t.Fatalf("UpsertEndpointAssigning(%s, %d): %v", key, id, err)
		}
		return got
	}
	first := upsert("a", 0)
	if again := upsert("a", 0); again != first {
		t.Errorf("a known source re-stated with 0 got %d, want its stored %d", again, first)
	}
	if top := upsert("top", 65534); top != 65534 {
		t.Fatalf("explicit top number: %d", top)
	}
	if wrapped := upsert("b", 0); wrapped != first+1 {
		t.Errorf("the allocation past the top got %d, want %d (wrapped to 2, %d held)", wrapped, first+1, first)
	}
	recs, err := s.ListEndpoints(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint16, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, r.EndpointID)
	}
	if !slices.IsSorted(ids) || len(ids) != 3 {
		t.Errorf("ListEndpoints numbers %v, want three, ascending", ids)
	}
}
