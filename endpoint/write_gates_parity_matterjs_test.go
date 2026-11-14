// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// The per-path rules of a write through im.HandleWriteRequest and the
// TopologyDispatcher, after matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts at db021da0.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
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
