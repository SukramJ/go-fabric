// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// An event path's node id is evaluated as an attribute path's is. Mirrors
// matter.js packages/protocol/src/action/server/EventReadResponse.ts at
// db91886c: a concrete path naming another node answers UnsupportedNode
// before any access check (#addConcrete :159-162), a wildcard path naming
// another node selects nothing (#addWildcard :138-140); the node's id is
// its id on the session's fabric, UNSPECIFIED without one
// (DataResponse.ts:57-62). matter.js has no test case for it; this test
// states those lines.

const (
	evtLocalNode = uint64(0x2222)
	evtOtherNode = uint64(0x9999)
)

// TestEventPathsForNode: a path naming another node is dropped, a path
// naming this node loses its node, a path naming none is kept as it is.
func TestEventPathsForNode(t *testing.T) {
	t.Parallel()
	startUp := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}
	other := startUp
	other.Node, other.HasNode = evtOtherNode, true
	local := startUp
	local.Node, local.HasNode = evtLocalNode, true
	wildOther := im.ConcreteEventPath{Node: evtOtherNode, HasNode: true}

	got := im.EventPathsForNode([]im.ConcreteEventPath{other, local, wildOther, startUp}, evtLocalNode)
	want := []im.ConcreteEventPath{startUp, startUp}
	if len(got) != len(want) {
		t.Fatalf("paths %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestDeniedEventPathStatusesUnsupportedNode: a concrete path naming
// another node answers UNSUPPORTED_NODE ahead of the access check; one
// naming this node and a wildcard naming another answer nothing here.
// Without the local node id stamped the node is not looked at.
func TestDeniedEventPathStatusesUnsupportedNode(t *testing.T) {
	t.Parallel()
	startUp := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x0028, Event: 0x00, HasEndpoint: true, HasCluster: true, HasEvent: true}
	other := startUp
	other.Node, other.HasNode = evtOtherNode, true
	local := startUp
	local.Node, local.HasNode = evtLocalNode, true
	wildOther := im.ConcreteEventPath{Node: evtOtherNode, HasNode: true}
	// A subject without any grant: the access check would deny every path.
	denyAll := im.EventReadAuthorizer{Checker: denyingACL{}, FabricIndex: 1, SubjectNodeID: 0x1111}
	allowAll := im.EventReadAuthorizer{PASE: true}
	ctx := im.WithLocalNodeID(context.Background(), evtLocalNode)

	got := im.DeniedEventPathStatuses(ctx, denyAll, []im.ConcreteEventPath{other})
	if len(got) != 1 || got[0].Status.Status != im.StatusUnsupportedNode {
		t.Fatalf("concrete path naming another node, no access: %+v, want one UNSUPPORTED_NODE", got)
	}
	if got := im.DeniedEventPathStatuses(ctx, allowAll, []im.ConcreteEventPath{other, local, wildOther}); len(got) != 1 || got[0].Status.Status != im.StatusUnsupportedNode || got[0].Path != other {
		t.Fatalf("paths naming another node and this one: %+v, want one UNSUPPORTED_NODE for %+v", got, other)
	}
	if got := im.DeniedEventPathStatuses(context.Background(), allowAll, []im.ConcreteEventPath{other}); len(got) != 0 {
		t.Fatalf("no local node id stamped: %+v, want nothing", got)
	}
}

// denyingACL grants nothing.
type denyingACL struct{}

func (denyingACL) CheckACL(context.Context, uint8, uint64, []uint32, uint16, uint32, uint8) im.StatusCode {
	return im.StatusUnsupportedAccess
}
