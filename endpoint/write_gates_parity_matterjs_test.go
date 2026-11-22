// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// The per-path rules of a write through im.HandleWriteRequest and the
// TopologyDispatcher, after matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts at db021da0.

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

const (
	gateOnOff  = uint32(0x0006)
	gateOnTime = uint32(0x4001) // writable uint16
)

func onTimeServer() *gateServer {
	return &gateServer{id: gateOnOff, attrs: []uint32{0x0000, gateOnTime}, vals: map[uint32]any{gateOnTime: uint16(0), 0x0000: false}}
}

func writeOf(p im.ConcreteAttributePath, v any) im.AttributeWrite {
	return im.AttributeWrite{Path: p, Value: im.AttributeValue{Value: v}}
}

// TestWriteNumericListIndexParity: a write path with a numeric ListIndex
// ends the interaction with INVALID_ACTION where the value would be
// written — after the existence, writability, access and DataVersion
// checks, the elements before it written, nothing written for it
// (AttributeWriteResponse.ts:419-424 at db021da0, thrown outside the try of
// writeValue).
func TestWriteNumericListIndexParity(t *testing.T) {
	t.Parallel()
	indexed := func(p im.ConcreteAttributePath) im.ConcreteAttributePath {
		p.HasListIndex, p.ListIndex = true, 3
		return p
	}
	wildcard := im.ConcreteAttributePath{Cluster: gateOnOff, Attribute: gateOnTime, HasCluster: true, HasAttribute: true}
	for _, c := range []struct {
		name       string
		writes     []im.AttributeWrite
		wantStatus im.StatusCode
		wantResp   []im.StatusCode
		wantWrites int
	}{
		{"concrete", []im.AttributeWrite{writeOf(indexed(concreteAttrPath(2, gateOnOff, gateOnTime)), uint16(7))}, im.StatusInvalidAction, nil, 0},
		{"after a written element", []im.AttributeWrite{
			writeOf(concreteAttrPath(2, gateOnOff, gateOnTime), uint16(5)),
			writeOf(indexed(concreteAttrPath(2, gateOnOff, gateOnTime)), uint16(7)),
			writeOf(concreteAttrPath(2, gateOnOff, gateOnTime), uint16(9)),
		}, im.StatusInvalidAction, []im.StatusCode{im.StatusSuccess}, 1},
		{"attribute absent: its status first", []im.AttributeWrite{writeOf(indexed(concreteAttrPath(2, gateOnOff, 0x4002)), uint16(7))}, im.StatusSuccess, []im.StatusCode{im.StatusUnsupportedAttribute}, 0},
		{"read-only attribute: its status first", []im.AttributeWrite{writeOf(indexed(concreteAttrPath(2, gateOnOff, 0x0000)), true)}, im.StatusSuccess, []im.StatusCode{im.StatusUnsupportedWrite}, 0},
		{"wildcard endpoint", []im.AttributeWrite{writeOf(indexed(wildcard), uint16(7))}, im.StatusInvalidAction, nil, 0},
	} {
		srv := onTimeServer()
		d := gateDispatcher(t, store.PrivilegeAdminister, srv)
		resp := im.HandleWriteRequest(caseCtx(false), d, im.WriteRequest{Writes: c.writes})
		if resp.Status != c.wantStatus {
			t.Errorf("%s: interaction status %v, want %v", c.name, resp.Status, c.wantStatus)
		}
		if len(resp.Responses) != len(c.wantResp) {
			t.Errorf("%s: responses %+v, want %v", c.name, resp.Responses, c.wantResp)
		} else {
			for i, r := range resp.Responses {
				if r.Status.Status != c.wantResp[i] {
					t.Errorf("%s: response %d status %v, want %v", c.name, i, r.Status.Status, c.wantResp[i])
				}
			}
		}
		if srv.writes != c.wantWrites {
			t.Errorf("%s: %d writes, want %d", c.name, srv.writes, c.wantWrites)
		}
	}

	// The timed check comes before it: an attribute with the timed quality
	// outside a timed interaction answers NEEDS_TIMED_INTERACTION on a
	// concrete path and is skipped on a wildcard one, the interaction going
	// on (AttributeWriteResponse.ts:243-247 and :343-345 before writeValue's
	// :419-424 at db021da0); inside a timed interaction the index ends it.
	for _, c := range []struct {
		name       string
		ctx        context.Context
		writes     []im.AttributeWrite
		wantStatus im.StatusCode
		wantResp   []im.StatusCode
	}{
		{"timed attribute, untimed, concrete", caseCtx(false), []im.AttributeWrite{
			writeOf(indexed(concreteAttrPath(2, gateOnOff, gateOnTime)), uint16(7)),
			writeOf(concreteAttrPath(2, gateOnOff, 0x0000), true),
		}, im.StatusSuccess, []im.StatusCode{im.StatusNeedsTimedInteraction, im.StatusUnsupportedWrite}},
		{"timed attribute, untimed, wildcard endpoint", caseCtx(false), []im.AttributeWrite{writeOf(indexed(wildcard), uint16(7))}, im.StatusSuccess, nil},
		{"timed attribute, timed, concrete", caseCtx(true), []im.AttributeWrite{writeOf(indexed(concreteAttrPath(2, gateOnOff, gateOnTime)), uint16(7))}, im.StatusInvalidAction, nil},
		{"timed attribute, timed, wildcard endpoint", caseCtx(true), []im.AttributeWrite{writeOf(indexed(wildcard), uint16(7))}, im.StatusInvalidAction, nil},
	} {
		srv := onTimeServer()
		d := gateDispatcher(t, store.PrivilegeAdminister, srv)
		d.timedWrite = &timedWriteTable{isTimed: func(c, a uint32) bool { return c == gateOnOff && a == gateOnTime }}
		resp := im.HandleWriteRequest(c.ctx, d, im.WriteRequest{Writes: c.writes})
		got := make([]im.StatusCode, len(resp.Responses))
		for i, r := range resp.Responses {
			got[i] = r.Status.Status
		}
		if resp.Status != c.wantStatus || !slices.Equal(got, c.wantResp) || srv.writes != 0 {
			t.Errorf("%s: interaction %v, responses %v after %d writes, want %v, %v after none", c.name, resp.Status, got, srv.writes, c.wantStatus, c.wantResp)
		}
	}

	// A group write takes the wildcard path: the element ends the group
	// interaction, nothing written for it.
	srv := onTimeServer()
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{groupEntry(1, store.PrivilegeManage, nil, nil)}})
	gctx := im.WithGroupSubject(im.WithFabricFilter(context.Background(), true, 1), im.GroupSubject{GroupID: 0x0101, HasValidMapping: true, Endpoints: []uint16{2}})
	if st := im.HandleGroupWriteRequest(gctx, d, im.WriteRequest{SuppressResponse: true, Writes: []im.AttributeWrite{writeOf(indexed(wildcard), uint16(7))}}); st != im.StatusInvalidAction || srv.writes != 0 {
		t.Errorf("group: status %v after %d writes, want INVALID_ACTION after none", st, srv.writes)
	}
}

// TestWriteOtherNodeParity: a write path naming a node other than this one
// is not written. A concrete path answers UNSUPPORTED_NODE before any other
// check (AttributeWriteResponse.ts:171-173 at db021da0); a wildcard or group
// path is dropped without a status (:121-123). The node is this node's id
// on the session's fabric (packages/protocol/src/action/server/DataResponse.ts:54-63).
func TestWriteOtherNodeParity(t *testing.T) {
	t.Parallel()
	const local, other = uint64(0x2222), uint64(0xDEADBEEF)
	onNode := func(p im.ConcreteAttributePath, node uint64) im.ConcreteAttributePath {
		p.Node, p.HasNode = node, true
		return p
	}
	wildcard := im.ConcreteAttributePath{Cluster: gateOnOff, Attribute: gateOnTime, HasCluster: true, HasAttribute: true}
	stamped := im.WithLocalNodeID(caseCtx(false), local)
	for _, c := range []struct {
		name       string
		ctx        context.Context
		path       im.ConcreteAttributePath
		want       []im.StatusCode
		wantWrites int
	}{
		{"concrete, another node", stamped, onNode(concreteAttrPath(2, gateOnOff, gateOnTime), other), []im.StatusCode{im.StatusUnsupportedNode}, 0},
		{"concrete, another node, endpoint absent: the node first", stamped, onNode(concreteAttrPath(42, gateOnOff, gateOnTime), other), []im.StatusCode{im.StatusUnsupportedNode}, 0},
		{"wildcard endpoint, another node", stamped, onNode(wildcard, other), nil, 0},
		{"concrete, this node", stamped, onNode(concreteAttrPath(2, gateOnOff, gateOnTime), local), []im.StatusCode{im.StatusSuccess}, 1},
		{"concrete, no node id stamped: not looked at", caseCtx(false), onNode(concreteAttrPath(2, gateOnOff, gateOnTime), other), []im.StatusCode{im.StatusSuccess}, 1},
	} {
		srv := onTimeServer()
		d := gateDispatcher(t, store.PrivilegeAdminister, srv)
		resp := im.HandleWriteRequest(c.ctx, d, im.WriteRequest{Writes: []im.AttributeWrite{writeOf(c.path, uint16(7))}})
		if len(resp.Responses) != len(c.want) {
			t.Errorf("%s: responses %+v, want %v", c.name, resp.Responses, c.want)
		} else {
			for i, r := range resp.Responses {
				if r.Status.Status != c.want[i] {
					t.Errorf("%s: response %d status %v, want %v", c.name, i, r.Status.Status, c.want[i])
				}
			}
		}
		if srv.writes != c.wantWrites {
			t.Errorf("%s: %d writes, want %d", c.name, srv.writes, c.wantWrites)
		}
	}

	// A group write takes the wildcard path: dropped without a status.
	srv := onTimeServer()
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{groupEntry(1, store.PrivilegeManage, nil, nil)}})
	gctx := im.WithLocalNodeID(im.WithGroupSubject(im.WithFabricFilter(context.Background(), true, 1), im.GroupSubject{GroupID: 0x0101, HasValidMapping: true, Endpoints: []uint16{2}}), local)
	if st := im.HandleGroupWriteRequest(gctx, d, im.WriteRequest{SuppressResponse: true, Writes: []im.AttributeWrite{writeOf(onNode(wildcard, other), uint16(7))}}); st != im.StatusSuccess || srv.writes != 0 {
		t.Errorf("group, another node: status %v after %d writes, want Success after none", st, srv.writes)
	}
}

// TestWriteAppendToNonListParity: an ADD (null ListIndex) to an attribute
// that is not a list answers INVALID_ACTION for its path
// (AttributeWriteResponse.ts:436-443 at db021da0, thrown inside the try of
// writeValue, :452-458); an element of the wrong type for a list stays
// CONSTRAINT_ERROR.
func TestWriteAppendToNonListParity(t *testing.T) {
	t.Parallel()
	add := func(p im.ConcreteAttributePath) im.ConcreteAttributePath {
		p.HasListIndex, p.ListAppend = true, true
		return p
	}
	const listAttr = uint32(0x4005)
	srv := &gateServer{id: gateOnOff, attrs: []uint32{gateOnTime, listAttr}, vals: map[uint32]any{gateOnTime: uint16(0), listAttr: []uint16{1}}}
	d := gateDispatcher(t, store.PrivilegeAdminister, srv)
	resp := im.HandleWriteRequest(caseCtx(false), d, im.WriteRequest{Writes: []im.AttributeWrite{
		writeOf(concreteAttrPath(2, gateOnOff, gateOnTime), uint16(7)),
		writeOf(add(concreteAttrPath(2, gateOnOff, gateOnTime)), []uint16{9}),
		writeOf(concreteAttrPath(2, gateOnOff, listAttr), []uint16{1}),
		writeOf(add(concreteAttrPath(2, gateOnOff, listAttr)), []string{"x"}),
	}})
	want := []im.StatusCode{im.StatusSuccess, im.StatusInvalidAction, im.StatusSuccess, im.StatusConstraintError}
	if len(resp.Responses) != len(want) {
		t.Fatalf("responses %+v, want %v", resp.Responses, want)
	}
	for i, r := range resp.Responses {
		if r.Status.Status != want[i] {
			t.Errorf("response %d: status %v, want %v", i, r.Status.Status, want[i])
		}
	}
}
