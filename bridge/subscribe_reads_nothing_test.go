// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for Bridge.subjectReadsNothing (subscribe.go), the
// decision that ends a subscription whose subject may read none of its
// paths (review II, R2-P3-admin-03, owner decision 53): it is reached on
// two consecutive observations of one unchanged ACL, a path that does not
// exist is no refusal, a failed read of the ACL decides nothing, and the
// walk over the paths runs only when the fabric's ACL or the served
// topology changed since the subscription last passed it (review II,
// R2-P3-dr-dev-01, -04, -05, -06).

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// readsNothingSubject is the CASE subject of the subscriptions below.
const readsNothingSubject uint64 = 0x1111

// countingOnOff is an OnOff server on the root endpoint that counts the
// reads of its OnOff attribute.
type countingOnOff struct{ reads atomic.Int64 }

func (*countingOnOff) MatterClusterID() uint32 { return 0x0006 }

func (c *countingOnOff) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	c.reads.Add(1)
	return false, true
}

func (*countingOnOff) MatterReportable() []uint32                             { return []uint32{0} }
func (*countingOnOff) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*countingOnOff) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

// switchableACL is an ACL source whose entries the test replaces and
// whose reads fail once failAfter reads were answered (0: never).
type switchableACL struct {
	mu        sync.Mutex
	entries   []store.ACLEntry
	reads     int
	failAfter int
}

func (s *switchableACL) set(entries ...store.ACLEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = entries
}

func (s *switchableACL) ListACL(_ context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.failAfter > 0 && s.reads > s.failAfter {
		return nil, errors.New("test: the ACL cannot be read")
	}
	var out []store.ACLEntry
	for _, e := range s.entries {
		if e.FabricIndex == fabricIndex {
			out = append(out, e)
		}
	}
	return out, nil
}

// viewEntry grants the test's subject View on fabric 1, on targets.
func viewEntry(targets ...store.ACLTarget) store.ACLEntry {
	return store.ACLEntry{
		FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{readsNothingSubject}, Targets: targets,
	}
}

// otherAdminEntry grants Administer on fabric 1 to another subject only.
func otherAdminEntry(subject uint64) store.ACLEntry {
	return store.ACLEntry{
		FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{subject},
	}
}

// readsNothingBridge is a started bridge serving srv on its root endpoint,
// its access checked against acl.
func readsNothingBridge(t *testing.T, acl *switchableACL, srv contract.ClusterServer) *Bridge {
	t.Helper()
	b := newStartedBridge(t)
	b.AttachRootClusters([]contract.ClusterServer{srv})
	b.AttachACLLister(acl)
	if err := b.Reassemble(t.Context()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	return b
}

// readsNothingSubscription is a CASE subscription of the test's subject on
// fabric 1 to paths.
func readsNothingSubscription(paths ...im.ConcreteAttributePath) (subTarget, *subscription.Subscription) {
	target := subTarget{fabricIndex: 1, subjectNodeID: readsNothingSubject, fabricFiltered: true}
	return target, &subscription.Subscription{ID: 7, FabricIndex: 1, AttributePaths: paths}
}

func rootOnOffPath() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
}

// TestSubjectReadsNothingTakesTwoObservationsOfOneACL: a subject refused
// every path is found to read nothing only at the second consecutive
// observation of the same ACL; an ACL that changed between two refusing
// observations starts the count again.
func TestSubjectReadsNothingTakesTwoObservationsOfOneACL(t *testing.T) {
	t.Parallel()
	acl := &switchableACL{}
	acl.set(otherAdminEntry(0xA0AD))
	b := readsNothingBridge(t, acl, &countingOnOff{})
	target, sub := readsNothingSubscription(rootOnOffPath())
	ctx := t.Context()

	if b.subjectReadsNothing(ctx, b.Dispatcher(), target, sub) {
		t.Fatal("the subject reads nothing after one observation of the ACL, want a second one first")
	}
	acl.set(otherAdminEntry(0xA0AE)) // changed, still refusing
	if b.subjectReadsNothing(ctx, b.Dispatcher(), target, sub) {
		t.Fatal("the subject reads nothing at an observation of an ACL that changed since the first, want the count started again")
	}
	if !b.subjectReadsNothing(ctx, b.Dispatcher(), target, sub) {
		t.Fatal("the subject refused every path at two observations of one ACL is not found to read nothing")
	}
}

// TestSubjectReadsNothingCountsNoPathThatIsGone: a concrete path whose
// endpoint the node no longer serves is no refusal, also for a subject
// whose grant names a device type the missing endpoint cannot match.
func TestSubjectReadsNothingCountsNoPathThatIsGone(t *testing.T) {
	t.Parallel()
	deviceType := uint32(0x0100)
	acl := &switchableACL{}
	acl.set(viewEntry(store.ACLTarget{DeviceType: &deviceType}))
	b := readsNothingBridge(t, acl, &countingOnOff{})
	gone := rootOnOffPath()
	gone.Endpoint = 9
	target, sub := readsNothingSubscription(gone)
	for i := range 3 {
		if b.subjectReadsNothing(t.Context(), b.Dispatcher(), target, sub) {
			t.Fatalf("observation %d: the subscription's only path names an endpoint the node does not serve, and that counts as a refusal", i+1)
		}
	}
}

// TestSubjectReadsNothingIsNotDecidedOnAFailedACLRead: the walk is
// judged against the entries the decision listed, so an ACL source that
// fails after that listing does not turn the subject's grant into
// refusals.
func TestSubjectReadsNothingIsNotDecidedOnAFailedACLRead(t *testing.T) {
	t.Parallel()
	acl := &switchableACL{failAfter: 1}
	acl.set(viewEntry())
	b := readsNothingBridge(t, acl, &countingOnOff{})
	target, sub := readsNothingSubscription(rootOnOffPath())
	for i := range 3 {
		if b.subjectReadsNothing(t.Context(), b.Dispatcher(), target, sub) {
			t.Fatalf("observation %d: the subject granted View reads nothing once the ACL source fails", i+1)
		}
	}
}

// TestSubjectReadsNothingWalksOnlyAfterAChange: the paths are read once
// for a subject that passed, and again only when the fabric's ACL or the
// served topology changed since.
func TestSubjectReadsNothingWalksOnlyAfterAChange(t *testing.T) {
	t.Parallel()
	acl := &switchableACL{}
	acl.set(viewEntry())
	srv := &countingOnOff{}
	b := readsNothingBridge(t, acl, srv)
	target, sub := readsNothingSubscription(rootOnOffPath())
	observe := func(wantReads int64, what string) {
		t.Helper()
		if b.subjectReadsNothing(t.Context(), b.Dispatcher(), target, sub) {
			t.Fatalf("%s: the subject granted View reads nothing", what)
		}
		if got := srv.reads.Load(); got != wantReads {
			t.Fatalf("%s: the paths were read %d times, want %d", what, got, wantReads)
		}
	}
	observe(1, "first observation")
	observe(1, "second observation of the unchanged ACL")
	observe(1, "third observation of the unchanged ACL")
	acl.set(viewEntry(), otherAdminEntry(0xA0AD))
	observe(2, "after an ACL change")
	observe(2, "after an ACL change, once more")
	if err := b.Reassemble(t.Context()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	observe(3, "after a change of the served topology")
}
