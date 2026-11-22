// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for resolveSessionFabric, SubscriptionReporter,
// and SubscriptionEventReporter. Lives in package bridge to access
// unexported methods.

import (
	"context"
	"testing"

	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
)

// fabricResolvingLookup implements both SessionLookup and SessionFabricResolver.
type fabricResolvingLookup struct {
	fabricMap map[uint16]uint8
}

func (f fabricResolvingLookup) Lookup(_ uint16) (*channel.Session, bool) { return nil, false }
func (f fabricResolvingLookup) FabricFor(sessionID uint16) (uint8, bool) {
	idx, ok := f.fabricMap[sessionID]
	return idx, ok
}

// ─── resolveSessionFabric ────────────────────────────────────────────────────

func TestResolveSessionFabric_ZeroSessionID_ReturnsZero(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	if idx := b.resolveSessionFabric(0); idx != 0 {
		t.Errorf("sessionID=0: want 0, got %d", idx)
	}
}

// TestResolveSessionFabric_NoResolver_IsUnresolvable pins review 2026-11
// W2-sec-03: a session whose fabric the bridge cannot name is
// FabricIndexUnresolvable — denied by the ACL gates — whether or not an ACL
// lister is attached, never 0.
func TestResolveSessionFabric_NoResolver_IsUnresolvable(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// noopSessionLookup does not implement SessionFabricResolver.
	b.AttachSessionLookup(nil) // revert to noop
	if idx := b.resolveSessionFabric(7); idx != endpointpkg.FabricIndexUnresolvable {
		t.Errorf("noop lookup: want FabricIndexUnresolvable, got %d", idx)
	}
}

func TestResolveSessionFabric_WithResolver_ReturnsCorrectFabric(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachSessionLookup(fabricResolvingLookup{
		fabricMap: map[uint16]uint8{5: 3},
	})
	if idx := b.resolveSessionFabric(5); idx != 3 {
		t.Errorf("sessionID=5: want fabricIndex=3, got %d", idx)
	}
}

func TestResolveSessionFabric_WithResolver_UnknownSession_ReturnsZero(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachSessionLookup(fabricResolvingLookup{
		fabricMap: map[uint16]uint8{5: 3},
	})
	if idx := b.resolveSessionFabric(99); idx != 0 {
		t.Errorf("unknown sessionID: want 0, got %d", idx)
	}
}

// ─── SubscriptionReporter / SubscriptionEventReporter ────────────────────────

func TestBridge_SubscriptionReporter_NotNil(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	if r := b.SubscriptionReporter(); r == nil {
		t.Error("SubscriptionReporter() returned nil")
	}
}

func TestBridge_SubscriptionEventReporter_NotNil(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	if r := b.SubscriptionEventReporter(); r == nil {
		t.Error("SubscriptionEventReporter() returned nil")
	}
}

// ─── reportSubscriptionEvents ────────────────────────────────────────────────

// TestReportSubscriptionEvents_NilSub_IsNoop verifies that calling
// reportSubscriptionEvents with a nil subscription returns without panicking.
func TestReportSubscriptionEvents_NilSub_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// nil sub → early return
	b.reportSubscriptionEvents(context.Background(), nil, nil)
}

// TestReportSubscriptionEvents_EmptyEvents_IsNoop verifies that an empty
// event list returns early without panicking.
func TestReportSubscriptionEvents_EmptyEvents_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// non-nil sub, empty events → early return
	b.reportSubscriptionEvents(context.Background(), &subscription.Subscription{}, nil)
}

// TestReportSubscriptionEvents_UnknownSubID_IsNoop verifies that when the
// subTarget is not registered, the function returns silently.
func TestReportSubscriptionEvents_UnknownSubID_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// Sub ID not in subTargets → Load returns ok=false → return
	b.reportSubscriptionEvents(context.Background(), &subscription.Subscription{ID: 99999},
		[]im.EventReport{{Path: im.ConcreteEventPath{Endpoint: 1}}})
}

// ─── releaseReportCounter ───────────────────────────────────────────────────

// ─── reportSubscription ──────────────────────────────────────────────────────

// TestReportSubscription_NilSub_IsNoop verifies that nil sub returns early.
func TestReportSubscription_NilSub_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.reportSubscription(context.Background(), nil, nil)
}

// TestReportSubscription_UnknownSubID_IsNoop verifies that missing
// subTarget returns early.
func TestReportSubscription_UnknownSubID_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.reportSubscription(context.Background(), &subscription.Subscription{ID: 77777},
		[]im.ConcreteAttributePath{{Endpoint: 0, HasEndpoint: true}})
}

// ─── noteReportUndelivered ──────────────────────────────────────────────────

// TestNoteReportUndelivered_WrongType_IsNoop verifies that if the
// stored value is not an *owedReport (wrong type assertion), the function
// returns without panicking.
func TestNoteReportUndelivered_WrongType_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// Store a string instead of *owedReport to force the type-assertion failure.
	b.reportCounterOwner.Store(reportCounterKey(0, 55), "not-an-owed-report")
	b.noteReportUndelivered(0, 55)
}

// TestNoteReportUndelivered_ZeroSubID_IsNoop verifies that a stored
// subID of 0 counts nothing and leaves no entry.
func TestNoteReportUndelivered_ZeroSubID_IsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.reportCounterOwner.Store(reportCounterKey(0, 66), &owedReport{})
	b.noteReportUndelivered(0, 66)
	// entry must be gone
	if _, ok := b.reportCounterOwner.Load(reportCounterKey(0, 66)); ok {
		t.Error("reportCounterOwner still has entry after noteReportUndelivered with subID=0")
	}
	if _, ok := b.subSendErrorCount.Load(uint32(0)); ok {
		t.Error("a failed update counted against subscription 0")
	}
}

// TestReleaseReportCounter_ZeroIsNoop verifies that calling
// releaseReportCounter(0) is a no-op and does not panic.
func TestReleaseReportCounter_ZeroIsNoop(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	// Pre-store something to confirm nothing is deleted for counter=0.
	b.reportCounterOwner.Store(reportCounterKey(0, 1), &owedReport{subID: 42})
	b.releaseReportCounter(0, 0) // must not panic, must not touch counter=1
	if _, ok := b.reportCounterOwner.Load(reportCounterKey(0, 1)); !ok {
		t.Error("releaseReportCounter(0) deleted an unrelated counter entry")
	}
}

// TestReleaseReportCounter_NonZeroDeletesEntry verifies that a non-zero
// counter is deleted from the map.
func TestReleaseReportCounter_NonZeroDeletesEntry(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.reportCounterOwner.Store(reportCounterKey(0, 99), &owedReport{subID: 7})
	b.releaseReportCounter(0, 99)
	if _, ok := b.reportCounterOwner.Load(reportCounterKey(0, 99)); ok {
		t.Error("releaseReportCounter(99): entry still present, expected deleted")
	}
}
