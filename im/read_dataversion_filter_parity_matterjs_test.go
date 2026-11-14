// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestReadParity_DataVersionFilterOnEveryVersion (review II, decision 46,
// R2-P2-ctr-05): a cluster whose version is 1 or 0 is left out for a filter
// that names it, as any other version is; a result without a version is
// never left out.
func TestReadParity_DataVersionFilterOnEveryVersion(t *testing.T) {
	t.Parallel()
	path := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
	for _, v := range []uint32{0, 1, 2} {
		d := &dataVersionDispatcher{readVal: AttributeValue{Value: true}, readStat: StatusSuccess, dataVersion: v}
		rd := HandleReadRequest(context.Background(), d, ReadRequest{
			AttributeRequests:  []ConcreteAttributePath{path},
			DataVersionFilters: []DataVersionFilter{{Endpoint: 1, Cluster: 0x0006, DataVersion: v}},
		})
		if len(rd.Reports) != 0 {
			t.Fatalf("version %d named by the filter: %d reports, want 0", v, len(rd.Reports))
		}
	}
	unversioned := &fakeDispatcher{readVal: AttributeValue{Value: true}, readStat: StatusSuccess}
	rd := HandleReadRequest(context.Background(), unversioned, ReadRequest{
		AttributeRequests:  []ConcreteAttributePath{path},
		DataVersionFilters: []DataVersionFilter{{Endpoint: 1, Cluster: 0x0006, DataVersion: 0}},
	})
	if len(rd.Reports) != 1 {
		t.Fatalf("a result without a version: %d reports, want 1", len(rd.Reports))
	}
}

// TestReadParity_DataVersionFilterForAnotherNodeIgnored: a DataVersionFilter
// whose path names another node does not leave the cluster out — matter.js
// packages/protocol/src/action/server/AttributeReadResponse.ts:73-77
// (`if (filterNodeId !== undefined && filterNodeId !== nodeId) continue;`).
// The node is decoded from the filter's ClusterPathIB (tag 0).
func TestReadParity_DataVersionFilterForAnotherNodeIgnored(t *testing.T) {
	t.Parallel()
	const local, other uint64 = 0x1111, 0x2222
	req := ReadRequest{
		AttributeRequests: []ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}},
		DataVersionFilters: []DataVersionFilter{
			{Endpoint: 1, Cluster: 0x0006, DataVersion: 5, NodeID: other, HasNodeID: true},
		},
	}
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalReadRequestTLV(tlv.NewDecoder(wire))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.DataVersionFilters) != 1 || !decoded.DataVersionFilters[0].HasNodeID || decoded.DataVersionFilters[0].NodeID != other {
		t.Fatalf("decoded filters %+v, want one naming node %#x", decoded.DataVersionFilters, other)
	}
	d := &dataVersionDispatcher{readVal: AttributeValue{Value: true}, readStat: StatusSuccess, dataVersion: 5}
	ctx := WithLocalNodeID(context.Background(), local)
	if rd := HandleReadRequest(ctx, d, decoded); len(rd.Reports) != 1 {
		t.Fatalf("filter for another node: %d reports, want 1", len(rd.Reports))
	}
	decoded.DataVersionFilters[0].NodeID = local
	if rd := HandleReadRequest(ctx, d, decoded); len(rd.Reports) != 0 {
		t.Fatalf("filter for this node: %d reports, want 0", len(rd.Reports))
	}
}
