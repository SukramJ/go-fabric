// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bindingdef "thingfabric.dev/go-fabric/cluster/spec/binding"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// pointerEntry and optionalOwnerEntry are the other two shapes an entry
// of a fabric-scoped list takes: a pointer element, and a FabricIndex the
// definition makes optional.
type (
	pointerEntry       struct{ V, FabricIndex uint8 }
	optionalOwnerEntry struct {
		V           uint8
		FabricIndex *uint8
	}
)

// TestWrittenFabricScopedListCarriesTheAccessingFabric (R2-P6-acl-0402,
// RW-i2): the value a server receives for a write of a fabric-scoped list
// carries the accessing fabric in every entry, whatever FabricIndex the
// request wrote — the dispatcher assigns it, in one place, for every
// server. The request's own value is not changed.
func TestWrittenFabricScopedListCarriesTheAccessingFabric(t *testing.T) {
	t.Parallel()
	path := concreteAttrPath(2, appendScopeCluster, appendScopeAttr)
	ctx := im.WithFabricFilter(context.Background(), false, 2)
	write := func(t *testing.T, item any) any {
		t.Helper()
		srv := &appendScopeServer{}
		d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
		res := d.WriteAuthorized(ctx, path, im.AttributeValue{Value: item}, nil)
		if len(res) != 1 || res[0].Status != im.StatusSuccess {
			t.Fatalf("results %+v, want one Success", res)
		}
		if len(srv.writes) != 1 {
			t.Fatalf("server saw %d writes", len(srv.writes))
		}
		return srv.writes[0]
	}
	t.Run("struct entries", func(t *testing.T) {
		t.Parallel()
		req := []ownedEntry{{5, 7}, {6, 0}}
		got := write(t, req)
		if want := []ownedEntry{{5, 2}, {6, 2}}; !slices.Equal(got.([]ownedEntry), want) {
			t.Errorf("server received %+v, want %+v", got, want)
		}
		if req[0].FabricIndex != 7 {
			t.Errorf("the request's value was changed: %+v", req)
		}
	})
	t.Run("pointer entries", func(t *testing.T) {
		t.Parallel()
		req := []*pointerEntry{{5, 7}}
		got := write(t, req).([]*pointerEntry)
		if got[0].FabricIndex != 2 || req[0].FabricIndex != 7 {
			t.Errorf("server received FabricIndex %d (request now %d), want 2 (request kept 7)", got[0].FabricIndex, req[0].FabricIndex)
		}
	})
	t.Run("an optional FabricIndex", func(t *testing.T) {
		t.Parallel()
		got := write(t, []optionalOwnerEntry{{V: 5}}).([]optionalOwnerEntry)
		if got[0].FabricIndex == nil || *got[0].FabricIndex != 2 {
			t.Errorf("server received %+v, want FabricIndex 2", got[0])
		}
	})
}

// TestStampAccessingFabricOnAGeneratedList: the stamp reaches the field
// the generated structs name FabricIndex (field 254), in the named slice
// type the generated decoder returns.
func TestStampAccessingFabricOnAGeneratedList(t *testing.T) {
	t.Parallel()
	req := spec.List[bindingdef.TargetStruct]{{FabricIndex: 1}, {FabricIndex: 9}}
	got, ok := stampAccessingFabric(req, 4).(spec.List[bindingdef.TargetStruct])
	if !ok {
		t.Fatalf("stamped value has type %T", stampAccessingFabric(req, 4))
	}
	for i, e := range got {
		if e.FabricIndex != 4 {
			t.Errorf("entry %d FabricIndex %d, want 4", i, e.FabricIndex)
		}
	}
	if req[0].FabricIndex != 1 {
		t.Errorf("the request's list was changed")
	}
	// Values without an owner field pass unchanged.
	for _, v := range []any{nil, uint8(3), []uint16{1}, spec.Node{}} {
		if got := stampAccessingFabric(v, 4); !reflect.DeepEqual(got, v) {
			t.Errorf("stamp(%#v) = %#v, want it unchanged", v, got)
		}
	}
}

// TestFabricScopedWriteWithoutAnAccessingFabricIsRefused (RW-i3): a write
// of an attribute a server declares fabric-scoped, from a request with no
// accessing fabric, is refused UNSUPPORTED_ACCESS on a concrete path and
// skipped on a wildcard one — replace and append alike — and the server
// is not called. im's gate covers the snapshot's attributes; this is the
// one place that knows a declarer's.
func TestFabricScopedWriteWithoutAnAccessingFabricIsRefused(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 0)
	for _, appendWrite := range []bool{false, true} {
		srv := &appendScopeServer{list: []ownedEntry{}}
		d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
		path := concreteAttrPath(2, appendScopeCluster, appendScopeAttr)
		path.ListAppend = appendWrite
		res := d.WriteAuthorized(ctx, path, im.AttributeValue{Value: []ownedEntry{{1, 0}}}, nil)
		if len(res) != 1 || res[0].Status != im.StatusUnsupportedAccess {
			t.Errorf("append=%v concrete: results %+v, want one UnsupportedAccess", appendWrite, res)
		}
		wild := path
		wild.HasEndpoint = false
		wild.Endpoint = 0
		if res := d.WriteAuthorized(ctx, wild, im.AttributeValue{Value: []ownedEntry{{1, 0}}}, nil); len(res) != 0 {
			t.Errorf("append=%v wildcard: results %+v, want the attribute skipped", appendWrite, res)
		}
		if len(srv.writes) != 0 {
			t.Errorf("append=%v: the server was called: %+v", appendWrite, srv.writes)
		}
	}
}
