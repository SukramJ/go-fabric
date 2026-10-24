// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package memstore_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/memstore"
)

var ctx = context.Background()

func upsert(t *testing.T, s *memstore.Store, key string, id uint16) uint16 {
	t.Helper()
	got, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey(key), Scope: "s", EndpointID: id, DeviceType: 0x100})
	if err != nil {
		t.Fatalf("upsert %s: %v", key, err)
	}
	return got
}

func TestRoundTripAndNotFound(t *testing.T) {
	s := memstore.New()
	if _, err := s.GetEndpoint(ctx, endpoint.StringKey("a")); !errors.Is(err, endpoint.ErrNotFound) {
		t.Fatalf("GetEndpoint on a miss = %v, want ErrNotFound", err)
	}
	if id := upsert(t, s, "a", 0); id != 2 {
		t.Fatalf("first number = %d, want 2", id)
	}
	// A known key keeps its number and takes the new scope and type.
	id, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("a"), Scope: "t", EndpointID: 40, DeviceType: 0x10E})
	if err != nil || id != 2 {
		t.Fatalf("re-upsert = %d, %v; want 2", id, err)
	}
	rec, err := s.GetEndpoint(ctx, endpoint.StringKey("a"))
	if err != nil || rec != (endpoint.Record{Key: endpoint.StringKey("a"), Scope: "t", EndpointID: 2, DeviceType: 0x10E}) {
		t.Fatalf("GetEndpoint = %+v, %v", rec, err)
	}
	if err := s.RemoveEndpoint(ctx, endpoint.StringKey("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEndpoint(ctx, endpoint.StringKey("a")); err != nil {
		t.Fatalf("a second remove = %v, want none", err)
	}
	if _, err := s.GetEndpoint(ctx, endpoint.StringKey("a")); !errors.Is(err, endpoint.ErrNotFound) {
		t.Fatalf("GetEndpoint after the remove = %v", err)
	}
}

func TestListIsOrderedAndScoped(t *testing.T) {
	s := memstore.New()
	upsert(t, s, "c", 9)
	upsert(t, s, "a", 3)
	if _, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("b"), Scope: "other", EndpointID: 5}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListEndpoints(ctx, "")
	if err != nil || len(all) != 3 || all[0].EndpointID != 3 || all[1].EndpointID != 5 || all[2].EndpointID != 9 {
		t.Fatalf("ListEndpoints(\"\") = %+v, %v", all, err)
	}
	scoped, err := s.ListEndpoints(ctx, "s")
	if err != nil || len(scoped) != 2 || scoped[0].Key.String() != "a" || scoped[1].Key.String() != "c" {
		t.Fatalf("ListEndpoints(s) = %+v, %v", scoped, err)
	}
}

// TestRemovedNumberIsNotReissued is ServerEndpointStores.ts assignNumber:
// the counter never rewinds.
func TestRemovedNumberIsNotReissued(t *testing.T) {
	s := memstore.New()
	upsert(t, s, "a", 0)
	b := upsert(t, s, "b", 0)
	if err := s.RemoveEndpoint(ctx, endpoint.StringKey("b")); err != nil {
		t.Fatal(err)
	}
	if id := upsert(t, s, "c", 0); id == b || id != 4 {
		t.Fatalf("number after removing %d = %d, want 4", b, id)
	}
}

func TestExplicitNumber(t *testing.T) {
	s := memstore.New()
	if id := upsert(t, s, "a", 100); id != 100 {
		t.Fatalf("explicit number = %d", id)
	}
	if id := upsert(t, s, "b", 0); id != 101 {
		t.Fatalf("number after an explicit 100 = %d, want 101", id)
	}
	for _, id := range []uint16{100, 65535} {
		if _, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey(fmt.Sprint("x", id)), EndpointID: id}); err == nil {
			t.Errorf("explicit number %d succeeded", id)
		}
	}
}

func TestWrapAndExhaustion(t *testing.T) {
	s := memstore.New()
	for i := 2; i <= 65534; i++ {
		if id := upsert(t, s, strconv.Itoa(i), 0); int(id) != i {
			t.Fatalf("allocation %d took %d", i, id)
		}
	}
	if _, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: endpoint.StringKey("full")}); !errors.Is(err, memstore.ErrEndpointsExhausted) {
		t.Fatalf("allocation on a full table = %v, want ErrEndpointsExhausted", err)
	}
	for _, k := range []string{"7", "70"} {
		if err := s.RemoveEndpoint(ctx, endpoint.StringKey(k)); err != nil {
			t.Fatal(err)
		}
	}
	if id := upsert(t, s, "wrapped", 0); id != 7 {
		t.Fatalf("number after the wrap = %d, want the first free 7", id)
	}
	if id := upsert(t, s, "next", 0); id != 70 {
		t.Fatalf("next number = %d, want 70", id)
	}
}

func TestConcurrentUse(t *testing.T) {
	s := memstore.New()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				key := endpoint.StringKey(fmt.Sprintf("%d-%d", g, i))
				if _, err := s.UpsertEndpointAssigning(ctx, endpoint.Record{Key: key, Scope: "s"}); err != nil {
					t.Error(err)
					return
				}
				if _, err := s.ListEndpoints(ctx, "s"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	all, _ := s.ListEndpoints(ctx, "")
	seen := map[uint16]bool{}
	for _, r := range all {
		if seen[r.EndpointID] {
			t.Fatalf("number %d issued twice", r.EndpointID)
		}
		seen[r.EndpointID] = true
	}
	if len(all) != 400 {
		t.Fatalf("%d rows, want 400", len(all))
	}
}
