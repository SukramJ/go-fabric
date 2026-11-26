// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors of shard 10, bridge/subscribe_dispatch.go
// (phase 8 wave 2b batch 2, #602, decision 692): the priming source's
// access and DataVersionFilter gates, primingMatches, the small helpers,
// the chunk loop's error returns and sendSubscribeResponse.

package bridge

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// accessControlClusterW2B2b is the AccessControl cluster newACLTestBridge
// mounts on the root endpoint.
const accessControlClusterW2B2b uint32 = 0x001F

// fabricSubjectCtxW2B2b is a CASE-like subject on fabric 1.
func fabricSubjectCtxW2B2b() context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
}

// drainPrimingW2B2b returns every report req's priming source yields.
func drainPrimingW2B2b(ctx context.Context, b *Bridge, req im.SubscribeRequest) []im.AttributeReport {
	src := b.newPrimingSource(ctx, b.Dispatcher(), req)
	var out []im.AttributeReport
	for rep, ok := src.next(); ok; rep, ok = src.next() {
		out = append(out, rep)
	}
	return out
}

// TestPrimingLeavesDeniedWildcardResultsOutW2B2b: a result the subject may
// not read is left out of a path with any wildcard — the attribute or the
// endpoint — and answered with a denial status only on a fully concrete
// path. Kills subscribe_dispatch.go:197 (|| → &&, both operators).
func TestPrimingLeavesDeniedWildcardResultsOutW2B2b(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE},
	}})
	ctx := fabricSubjectCtxW2B2b()
	for _, path := range []im.ConcreteAttributePath{
		{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: accessControlClusterW2B2b},        // wildcard attribute
		{HasCluster: true, Cluster: accessControlClusterW2B2b, HasAttribute: true, Attribute: 0x0000}, // wildcard endpoint, ACL (Administer)
	} {
		for _, rep := range drainPrimingW2B2b(ctx, b, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{path}}) {
			if rep.IsStatus {
				t.Errorf("path %+v: a denied result of a wildcard path was answered %v at %+v", path, rep.Status.Status, rep.Path)
			}
		}
	}
	// The concrete ACL attribute itself is answered with its denial.
	reps := drainPrimingW2B2b(ctx, b, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{accessControlACLPath()}})
	if len(reps) != 1 || !reps[0].IsStatus {
		t.Fatalf("the concrete denied path was answered %+v, want one status", reps)
	}
}

// TestPrimingDataVersionFilterSkipsOnlyAnUnchangedClusterW2B2b: a cluster
// at its filter's version is left out, one at another version reported, a
// status reported whatever the filter. Kills subscribe_dispatch.go:241
// (== → !=, the second && → ||) and :243 (guard off, == → !=).
func TestPrimingDataVersionFilterSkipsOnlyAnUnchangedClusterW2B2b(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{})
	ctx := im.WithAuthModePASE(context.Background())
	values := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: accessControlClusterW2B2b, HasAttribute: true, Attribute: 0x0000}
	missing := values
	missing.Attribute = 0x0FFE // no such attribute: a status
	plain := drainPrimingW2B2b(ctx, b, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{values}})
	if len(plain) != 1 || plain[0].IsStatus || !plain[0].HasDataVersion {
		t.Fatalf("unfiltered priming of the ACL attribute = %+v, want one value with a DataVersion", plain)
	}
	v := plain[0].DataVersion
	filtered := func(version uint32) []im.AttributeReport {
		return drainPrimingW2B2b(ctx, b, im.SubscribeRequest{
			AttributeRequests:  []im.ConcreteAttributePath{missing, values},
			DataVersionFilters: []im.DataVersionFilter{{Endpoint: 0, Cluster: accessControlClusterW2B2b, DataVersion: version}},
		})
	}
	if got := filtered(v); len(got) != 1 || !got[0].IsStatus {
		t.Errorf("filter at the cluster's version: reports %+v, want only the missing attribute's status", got)
	}
	if got := filtered(v + 1); len(got) != 2 {
		t.Errorf("filter at another version: %d reports, want the status and the value", len(got))
	}
}

// TestPrimingDataVersionFiltersForAnotherNodeW2B2b: a filter naming
// another node is ignored. Kills subscribe_dispatch.go:177 (guard off).
func TestPrimingDataVersionFiltersForAnotherNodeW2B2b(t *testing.T) {
	t.Parallel()
	ctx := im.WithLocalNodeID(context.Background(), 0xAAAA)
	got := primingDataVersionFilters(ctx, []im.DataVersionFilter{{Endpoint: 0, Cluster: 6, DataVersion: 1, NodeID: 0xBBBB, HasNodeID: true}})
	if len(got) != 0 {
		t.Fatalf("filters %+v kept, want the other node's filter dropped", got)
	}
}

// nonACLDispatcherW2B2b is a dispatcher with no access checker.
type nonACLDispatcherW2B2b struct{ im.Dispatcher }

// TestReadDenialStatusWithoutCheckerOrFabricW2B2b: with no access checker,
// or no accessing fabric, the denial is UnsupportedAccess. Kills
// subscribe_dispatch.go:158 (guard off, || → &&).
func TestReadDenialStatusWithoutCheckerOrFabricW2B2b(t *testing.T) {
	t.Parallel()
	rr := im.ReadResult{Path: accessControlACLPath()}
	if got := readDenialStatus(fabricSubjectCtxW2B2b(), nonACLDispatcherW2B2b{}, rr); got != im.StatusUnsupportedAccess {
		t.Errorf("no checker: %v, want UnsupportedAccess", got)
	}
	if got := readDenialStatus(context.Background(), nonACLDispatcherW2B2b{}, rr); got != im.StatusUnsupportedAccess {
		t.Errorf("no checker, no fabric: %v, want UnsupportedAccess", got)
	}
}

// TestPrimingMatchesOnEventPathsW2B2b: an event-only Subscribe matches
// when the subject may read one of its events and not otherwise. Kills
// subscribe_dispatch.go:370 (guard off, && → ||, the second > → >=), the
// three rows node killed (bridge-blind).
func TestPrimingMatchesOnEventPathsW2B2b(t *testing.T) {
	t.Parallel()
	aclEvent := im.SubscribeRequest{EventRequests: []im.ConcreteEventPath{{
		Endpoint: 0, HasEndpoint: true, Cluster: accessControlClusterW2B2b, HasCluster: true, Event: 0x00, HasEvent: true,
	}}}
	ctx := fabricSubjectCtxW2B2b()
	view := newACLTestBridge(t, &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE},
	}})
	if view.primingMatches(ctx, view.Dispatcher(), aclEvent) {
		t.Error("a View subject's AccessControlEntryChanged subscription matched")
	}
	admin := newACLTestBridge(t, &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE},
	}})
	if !admin.primingMatches(ctx, admin.Dispatcher(), aclEvent) {
		t.Error("an Administer subject's AccessControlEntryChanged subscription did not match")
	}
}

// TestLatestEventNumberOfTheDataEventsW2B2b: the highest number among the
// events with data, statuses aside, in any order. Kills
// subscribe_dispatch.go:591 (&& → ||, || → &&, drop ! twice).
func TestLatestEventNumberOfTheDataEventsW2B2b(t *testing.T) {
	t.Parallel()
	got, ok := latestEventNumber([]im.EventReport{{IsStatus: true, Number: 99}, {Number: 5}, {Number: 9}, {Number: 7}})
	if !ok || got != 9 {
		t.Errorf("latestEventNumber = %d, %v; want 9, true", got, ok)
	}
	if got, ok := latestEventNumber([]im.EventReport{{IsStatus: true, Number: 4}}); ok {
		t.Errorf("statuses only: %d, true; want none", got)
	}
}

// TestSubscriptionPeerOfAnUnsecuredFrameW2B2b: a frame of the unsecured
// session is attributed to its header's source node id. Kills
// subscribe_dispatch.go:608 (guard off).
func TestSubscriptionPeerOfAnUnsecuredFrameW2B2b(t *testing.T) {
	t.Parallel()
	if got := subscriptionPeerNodeID(&message.Header{HasSourceNodeID: true, SourceNodeID: 0x5A}, 0); got != 0x5A {
		t.Fatalf("peer = %#x, want the header's 0x5a", got)
	}
}

// TestEventPathAllowedWithoutDispatcherW2B2b: without a dispatcher every
// event path counts. Kills subscribe_dispatch.go:938 (guard off).
func TestEventPathAllowedWithoutDispatcherW2B2b(t *testing.T) {
	t.Parallel()
	if !eventPathAllowed(context.Background(), nil, im.EventReadAuthorizer{}, im.ConcreteEventPath{}) {
		t.Fatal("an event path without a dispatcher does not count")
	}
}

// TestSubscribeResponseMaxIntervalIsTheSubscriptionsW2B2b: the
// SubscribeResponse announces the subscription's own MaxIntervalCeiling,
// and without a manager the requested one. Kills subscribe_dispatch.go:799
// (&& → ||, != → == twice), :800 (== → !=) and :883 (&& → ||); the three
// != / == rows node killed (bridge-blind).
func TestSubscribeResponseMaxIntervalIsTheSubscriptionsW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	sub, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 0, PeerNodeID: 0x5A5A, MaxIntervalCeiling: 100, AttributePaths: noMatchPathsW2B2b(1), Priming: true})
	if err != nil {
		t.Fatal(err)
	}
	send := func(b *Bridge, subID uint32) uint16 {
		t.Helper()
		c := &fakeConn{}
		hdr := &message.Header{MessageCounter: 3, HasSourceNodeID: true, SourceNodeID: 0x5A5A}
		proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 0x5400, Initiator: true}
		if err := b.sendSubscribeResponse(&connRoute{conn: c}, hdr, proto, im.SubscribeRequest{MaxIntervalCeiling: 60}, subID, im.ReportData{}); err != nil {
			t.Fatalf("sendSubscribeResponse: %v", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.sent) != 1 {
			t.Fatalf("%d datagrams sent, want the SubscribeResponse", len(c.sent))
		}
		_, hdrLen, err := message.UnmarshalHeader(c.sent[0])
		if err != nil {
			t.Fatal(err)
		}
		_, protoLen, err := message.UnmarshalProtocolHeader(c.sent[0][hdrLen:])
		if err != nil {
			t.Fatal(err)
		}
		resp, err := im.UnmarshalSubscribeResponseTLV(tlv.NewDecoder(c.sent[0][hdrLen+protoLen:]))
		if err != nil {
			t.Fatal(err)
		}
		return resp.MaxInterval
	}
	if got := send(b, sub.ID); got != 100 {
		t.Errorf("MaxInterval = %d, want the subscription's 100", got)
	}
	if got := send(unstartedBridgeW2B2b(t), 7); got != 60 {
		t.Errorf("without a manager: MaxInterval = %d, want the requested 60", got)
	}
}
