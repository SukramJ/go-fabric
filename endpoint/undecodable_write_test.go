// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestUndecodableValueReachesNoServer (owner decision 161): an
// im.UndecodableValue reaches no cluster server — a direct Write answers
// FAILURE on the path; through im the unicast interaction ends with
// FAILURE, the attribute data before it applied, and a group write ends
// with FAILURE too.
func TestUndecodableValueReachesNoServer(t *testing.T) {
	t.Parallel()
	bad := im.AttributeValue{Value: im.UndecodableValue{Err: errors.New("a structure to a scalar")}}
	newDispatcher := func() (*TopologyDispatcher, *appendScopeServer) {
		srv := &appendScopeServer{}
		d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
		d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(2, store.PrivilegeAdminister, nil), groupEntry(2, store.PrivilegeOperate, nil, nil)}})
		return d, srv
	}
	path := concreteAttrPath(2, appendScopeCluster, appendScopeAttr)
	ctx := im.WithSubject(im.WithTimedInteraction(im.WithFabricFilter(context.Background(), false, 2), false), 0x2222, nil)

	d, srv := newDispatcher()
	if res := d.Write(ctx, path, bad); len(res) != 1 || res[0].Status != im.StatusFailure {
		t.Errorf("direct Write: %+v, want one FAILURE", res)
	}
	if len(srv.writes) != 0 {
		t.Errorf("the server received %+v", srv.writes)
	}

	d, srv = newDispatcher()
	good := im.AttributeValue{Value: []ownedEntry{{1, 2}}}
	wr := im.HandleWriteRequest(ctx, d, im.WriteRequest{Writes: []im.AttributeWrite{{Path: path, Value: good}, {Path: path, Value: bad}}})
	if wr.Status != im.StatusFailure || len(wr.Responses) != 1 || wr.Responses[0].Status.Status != im.StatusSuccess {
		t.Errorf("unicast: status %v responses %+v, want FAILURE after the first data's SUCCESS", wr.Status, wr.Responses)
	}
	if len(srv.writes) != 1 {
		t.Errorf("unicast: the server received %d writes, want the first only", len(srv.writes))
	}

	d, srv = newDispatcher()
	wild := path
	wild.HasEndpoint, wild.Endpoint = false, 0
	gctx := im.WithGroupSubject(im.WithFabricFilter(context.Background(), true, 2), im.GroupSubject{GroupID: 1, HasValidMapping: true, Endpoints: []uint16{2}})
	out := im.HandleGroupWrite(gctx, d, im.WriteRequest{SuppressResponse: true, Writes: []im.AttributeWrite{{Path: wild, Value: bad}}})
	if out.Status != im.StatusFailure || len(srv.writes) != 0 {
		t.Errorf("group: status %v, server writes %d — want FAILURE and none", out.Status, len(srv.writes))
	}
}
