// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// White-box tests for Bridge.authorizedEventReports (subscribe.go) — the
// ongoing event fan-out ACL + fabric-sensitive gate. Uses the same real
// AccessControl-on-root TopologyDispatcher scaffolding as subscribe_acl_test.go
// (newACLTestBridge / aclStoreFake) so the event path exercises the exact
// MinReadPrivilege / CheckACL wiring production uses, plus a real
// core.AccessControlEntryChangedEvent payload for the fabric-index extraction.

package bridge

import (
	"context"
	"testing"

	core "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// acEntryChangedReport builds an EventReport for the fabric-sensitive
// AccessControl.AccessControlEntryChanged event (endpoint 0, cluster 0x001F,
// event 0x00) whose payload record belongs to recordFabric.
func acEntryChangedReport(recordFabric uint8) im.EventReport {
	return im.EventReport{
		Path: im.ConcreteEventPath{
			Endpoint: 0, HasEndpoint: true,
			Cluster: 0x001F, HasCluster: true,
			Event: 0x00, HasEvent: true,
		},
		Number:   3,
		Priority: im.EventPriority(1),
		Data:     im.AttributeValue{Value: core.AccessControlEntryChangedEvent{FabricIndex: recordFabric}},
	}
}

// TestAuthorizedEventReports_FabricSensitiveDroppedCrossFabric verifies that a
// fabric-2 subject holding Administer does NOT receive a fabric-1
// AccessControlEntryChanged event, while the owning fabric-1 subject does —
// path-ACL alone (both hold Administer) would otherwise leak the record across
// fabrics (Matter §8.4.3.2 / §9.10.7.1).
func TestAuthorizedEventReports_FabricSensitiveDroppedCrossFabric(t *testing.T) {
	t.Parallel()
	fake := &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE},
		{FabricIndex: 2, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE},
	}}
	b := newACLTestBridge(t, fake)
	rec := acEntryChangedReport(1) // record owned by fabric 1

	// Owning fabric 1 (Administer) → kept.
	owning := subTarget{fabricIndex: 1, subjectNodeID: 0x1111}
	if got := b.authorizedEventReports(context.Background(), owning, []im.EventReport{rec}); len(got) != 1 {
		t.Fatalf("owning fabric 1: want 1 event, got %d", len(got))
	}

	// Foreign fabric 2 (Administer, but not the owner) → dropped.
	foreign := subTarget{fabricIndex: 2, subjectNodeID: 0x2222}
	if got := b.authorizedEventReports(context.Background(), foreign, []im.EventReport{rec}); len(got) != 0 {
		t.Fatalf("foreign fabric 2 must not receive fabric 1's fabric-sensitive event: got %d", len(got))
	}
}

// TestAuthorizedEventReports_NonAdministerDenied verifies that a fabric-1
// subject holding only View is denied the AccessControl event (Administer
// required), even though the record belongs to its own fabric.
func TestAuthorizedEventReports_NonAdministerDenied(t *testing.T) {
	t.Parallel()
	fake := &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE},
	}}
	b := newACLTestBridge(t, fake)
	rec := acEntryChangedReport(1)

	target := subTarget{fabricIndex: 1, subjectNodeID: 0x1111}
	if got := b.authorizedEventReports(context.Background(), target, []im.EventReport{rec}); len(got) != 0 {
		t.Fatalf("View-only subject must be denied AccessControl events: got %d", len(got))
	}
}

// TestAuthorizedEventReports_PASEBypass verifies that a PASE session
// (fabricIndex==0) receives the event unchanged — ACL/fabric filtering does not
// apply before commissioning.
func TestAuthorizedEventReports_PASEBypass(t *testing.T) {
	t.Parallel()
	fake := &aclStoreFake{}
	b := newACLTestBridge(t, fake)
	rec := acEntryChangedReport(1)

	target := subTarget{fabricIndex: 0}
	if got := b.authorizedEventReports(context.Background(), target, []im.EventReport{rec}); len(got) != 1 {
		t.Fatalf("PASE bypass: want 1 event, got %d", len(got))
	}
}

// TestSubscribeRejectsEventPathsTheSubjectMayNotRead pins the establish
// decision for an event-only subscription: a path counts only when it
// resolves to a cluster whose events the subject may read. A View-only
// subject subscribing to AccessControlEntryChanged (Administer) matches
// nothing, so the subscription is rejected with INVALID_ACTION, as matter.js
// does (EventReadResponse.ts counts.existent excludes denied paths;
// ServerSubscription.ts "no attributes or events are matching"). TC-ACE-1.2
// step 14 expects it. An Administer subject's identical subscription counts.
func TestSubscribeRejectsEventPathsTheSubjectMayNotRead(t *testing.T) {
	t.Parallel()
	fake := &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE},
	}}
	b := newACLTestBridge(t, fake)
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)

	aclEvent := im.SubscribeRequest{EventRequests: []im.ConcreteEventPath{{
		Endpoint: 0, HasEndpoint: true, Cluster: 0x001F, HasCluster: true, Event: 0x00, HasEvent: true,
	}}}
	if _, matched := b.buildInitialReport(ctx, b.Dispatcher(), aclEvent); matched != 0 {
		t.Fatalf("View-only subject: AccessControlEntryChanged matched %d path(s), want 0 (INVALID_ACTION)", matched)
	}

	admin := newACLTestBridge(t, &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE},
	}})
	if _, matched := admin.buildInitialReport(ctx, admin.Dispatcher(), aclEvent); matched != 1 {
		t.Fatalf("Administer subject: AccessControlEntryChanged matched %d path(s), want 1", matched)
	}
}

// TestConcreteDeniedEventPathIsAnsweredWithAStatus pins that a concrete event
// path the subject may not read is answered with UNSUPPORTED_ACCESS rather
// than left out (matter.js EventReadResponse.ts #addConcrete); a wildcard is
// still skipped silently. TC-ACL-2.9 step 11.
func TestConcreteDeniedEventPathIsAnsweredWithAStatus(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{entries: []store.ACLEntry{
		{FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE},
	}})
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
	auth := b.eventReadAuthorizer(b.Dispatcher(), 1, false, 0x1111, nil)
	got := im.DeniedEventPathStatuses(ctx, auth, []im.ConcreteEventPath{
		{Endpoint: 0, HasEndpoint: true, Cluster: 0x001F, HasCluster: true, Event: 0, HasEvent: true},
		{Endpoint: 0, HasEndpoint: true, Cluster: 0x001F, HasCluster: true}, // wildcard event
	})
	if len(got) != 1 || !got[0].IsStatus || got[0].Status.Status != im.StatusUnsupportedAccess {
		t.Fatalf("statuses %+v, want one UNSUPPORTED_ACCESS for the concrete path", got)
	}
}
