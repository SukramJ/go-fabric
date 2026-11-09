// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// ---- helpers ----

// mkEventPath builds a ConcreteEventPath.  Pass 0 for HasEndpoint=false,
// HasCluster=false or HasEvent=false by omitting the dimension from the
// Has* flags — use the named args pattern below for clarity.
func mkEventPath(endpoint uint16, cluster, event uint32) im.ConcreteEventPath {
	return im.ConcreteEventPath{
		Endpoint:    endpoint,
		Cluster:     cluster,
		Event:       event,
		HasEndpoint: true,
		HasCluster:  true,
		HasEvent:    true,
	}
}

// mkEventFiring builds an EventFiring with the given priority.
func mkEventFiring(endpoint uint16, cluster, event uint32, priority im.EventPriority) subscription.EventFiring {
	return subscription.EventFiring{
		Path:     mkEventPath(endpoint, cluster, event),
		Number:   1,
		Priority: priority,
	}
}

// newEventManager builds a manager whose event source is a fresh event
// log, and the function that emits an event the way the bridge does:
// append to the log, then fan the numbered firing out.
func newEventManager(cfg subscription.Config) (m *subscription.Manager, fire func(subscription.EventFiring)) {
	m = subscription.NewManager(cfg, nil, nil)
	log := im.NewEventLog()
	m.SetEventSource(log)
	return m, func(ev subscription.EventFiring) {
		ev.Number = log.Append(im.EventRecord{
			Priority: ev.Priority,
			Endpoint: ev.Path.Endpoint,
			Cluster:  ev.Path.Cluster,
			EventID:  ev.Path.Event,
			Payload:  uint8(1),
		})
		m.OnEventFired(ev)
	}
}

// eventReporterCall captures one EventReporter invocation.
type eventReporterCall struct {
	sub    *subscription.Subscription
	events []im.EventReport
}

// chanEventReporter returns an EventReporter that sends to ch.
func chanEventReporter(ch chan eventReporterCall) subscription.EventReporter {
	return func(_ context.Context, sub *subscription.Subscription, events []im.EventReport) {
		ch <- eventReporterCall{sub: sub, events: events}
	}
}

// flushReporterCall is one [subscription.FlushReporter] call.
type flushReporterCall struct {
	sub    *subscription.Subscription
	events []im.EventReport
	paths  []im.ConcreteAttributePath
}

// chanFlushReporter returns a FlushReporter that sends to ch.
func chanFlushReporter(ch chan flushReporterCall) subscription.FlushReporter {
	return func(_ context.Context, sub *subscription.Subscription, events []im.EventReport, paths []im.ConcreteAttributePath) {
		ch <- flushReporterCall{sub: sub, events: events, paths: paths}
	}
}

// defaultEventArgs builds SubscribeArgs with a single event path covering
// (endpoint=1, cluster=0x003B, event=0x01) and a 1s MinIntervalFloor.
func defaultEventArgs() subscription.SubscribeArgs {
	return subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xBEEF,
		SessionID:          10,
		MinIntervalFloor:   1,
		MaxIntervalCeiling: 60,
		AttributePaths:     []im.ConcreteAttributePath{}, // no attribute paths
		EventPaths: []im.ConcreteEventPath{
			mkEventPath(1, 0x003B, 0x01),
		},
	}
}

// ---- Test: matching event path → reporter receives 1 EventReport ----

func TestOnEventFired_MatchingPath_ReporterReceivesOne(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 4)
	m, fire := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(ch))

	sub, err := m.Subscribe(defaultEventArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Fire matching event.
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityCritical))

	ctx := context.Background()
	// Critical priority → bypasses MinIntervalFloor; Tick with t0+0 is enough.
	m.Tick(ctx, time.Now())

	select {
	case call := <-ch:
		if call.sub.ID != sub.ID {
			t.Errorf("wrong sub: %d vs %d", call.sub.ID, sub.ID)
		}
		if len(call.events) != 1 {
			t.Errorf("expected 1 event, got %d", len(call.events))
		}
	default:
		t.Fatal("EventReporter not called after matching OnEventFired + Tick")
	}
}

// ---- Test: non-matching event path → reporter receives 0 ----

func TestOnEventFired_NonMatchingPath_ReporterSilent(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 4)
	m, fire := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(ch))

	// Subscribe for (endpoint=1, cluster=0x003B, event=0x01).
	_, err := m.Subscribe(defaultEventArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Fire a different cluster — no match.
	fire(mkEventFiring(1, 0x0006, 0x01, im.EventPriorityCritical))

	ctx := context.Background()
	m.Tick(ctx, time.Now())

	if len(ch) != 0 {
		t.Fatalf("EventReporter called %d time(s), want 0 for non-matching path", len(ch))
	}
}

// ---- Test: two subscriptions with overlapping wildcards → both receive ----

func TestOnEventFired_TwoSubscriptions_BothReceive(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 8)
	m, fire := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(ch))

	// Subscription A: wildcard endpoint, concrete cluster+event.
	argsA := subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xA,
		SessionID:          11,
		MinIntervalFloor:   1,
		MaxIntervalCeiling: 60,
		AttributePaths:     []im.ConcreteAttributePath{},
		EventPaths: []im.ConcreteEventPath{
			{Cluster: 0x003B, HasCluster: true, Event: 0x01, HasEvent: true},
		},
	}
	// Subscription B: concrete endpoint+cluster, wildcard event.
	argsB := subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xB,
		SessionID:          12,
		MinIntervalFloor:   1,
		MaxIntervalCeiling: 60,
		AttributePaths:     []im.ConcreteAttributePath{},
		EventPaths: []im.ConcreteEventPath{
			{Endpoint: 1, HasEndpoint: true, Cluster: 0x003B, HasCluster: true},
		},
	}

	if _, err := m.Subscribe(argsA); err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}
	if _, err := m.Subscribe(argsB); err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}

	// Fire concrete event (endpoint=1, cluster=0x003B, event=0x01). Both must match.
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityCritical))

	ctx := context.Background()
	m.Tick(ctx, time.Now())

	// Drain up to 2 calls.
	var calls []eventReporterCall
	for range 2 {
		select {
		case c := <-ch:
			calls = append(calls, c)
		default:
		}
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 EventReporter calls (one per sub), got %d", len(calls))
	}
}

// ---- Test: Critical priority bypasses MinIntervalFloor ----

func TestOnEventFired_CriticalPriority_BypassesMinIntervalFloor(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 4)
	// Use a large MinIntervalFloor (10 s) that would normally block.
	cfg := subscription.Config{MinIntervalFloorSeconds: 10}
	m, fire := newEventManager(cfg)
	m.SetEventReporter(chanEventReporter(ch))

	args := subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xC,
		SessionID:          20,
		MinIntervalFloor:   10, // large floor
		MaxIntervalCeiling: 60,
		AttributePaths:     []im.ConcreteAttributePath{},
		EventPaths:         []im.ConcreteEventPath{mkEventPath(1, 0x003B, 0x01)},
	}
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ctx := context.Background()
	t0 := time.Now()
	// Establish lastReport via a Tick (keepalive because MaxInterval elapsed).
	m.Tick(ctx, t0)
	for len(ch) > 0 {
		<-ch
	}

	// Fire Critical event just 1s after last report (well within the 10s floor).
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityCritical))
	m.Tick(ctx, t0.Add(1*time.Second))

	if len(ch) == 0 {
		t.Fatal("EventReporter not called for Critical event within MinIntervalFloor window")
	}
}

// ---- Test: non-Critical events wait for MinIntervalFloor ----

func TestOnEventFired_InfoPriority_WaitsForMinIntervalFloor(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 4)
	cfg := subscription.Config{MinIntervalFloorSeconds: 5}
	m, fire := newEventManager(cfg)
	m.SetEventReporter(chanEventReporter(ch))

	args := subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xD,
		SessionID:          30,
		MinIntervalFloor:   5,
		MaxIntervalCeiling: 60,
		AttributePaths:     []im.ConcreteAttributePath{},
		EventPaths:         []im.ConcreteEventPath{mkEventPath(1, 0x003B, 0x01)},
	}
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ctx := context.Background()
	t0 := time.Now()
	// Establish lastReport.
	m.Tick(ctx, t0)
	for len(ch) > 0 {
		<-ch
	}

	// Fire Info-priority event.
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))

	// Tick only 2s later — MinIntervalFloor (5s) not yet elapsed.
	m.Tick(ctx, t0.Add(2*time.Second))
	if len(ch) != 0 {
		t.Fatal("EventReporter fired before MinIntervalFloor elapsed for non-Critical event")
	}

	// Tick after 6s — floor elapsed now.
	m.Tick(ctx, t0.Add(6*time.Second))
	if len(ch) == 0 {
		t.Fatal("EventReporter not called after MinIntervalFloor elapsed for Info event")
	}
}

// ---- Test: OnEventFired with non-concrete path (HasEndpoint=false) is a no-op ----

func TestOnEventFired_NonConcretePath_IsNoOp(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 4)
	m, _ := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(ch))

	_, err := m.Subscribe(defaultEventArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Path without HasEndpoint=true — must be rejected by OnEventFired.
	nonConcrete := subscription.EventFiring{
		Path: im.ConcreteEventPath{
			// HasEndpoint deliberately false
			Cluster: 0x003B, HasCluster: true,
			Event: 0x01, HasEvent: true,
		},
		Priority: im.EventPriorityCritical,
	}
	m.OnEventFired(nonConcrete)

	ctx := context.Background()
	m.Tick(ctx, time.Now())

	if len(ch) != 0 {
		t.Fatalf("EventReporter called %d time(s) for non-concrete path, want 0", len(ch))
	}
}

// FlushFabric reports what the fabric's subscriptions owe at once — an
// Info event behind its min interval included — and leaves the invoking
// session's and the other fabrics' subscriptions alone: matter.js
// Fabric.leave's closeSubscriptions(true) on every session of the fabric
// (packages/protocol/src/fabric/Fabric.ts:447-457 at 862ad75f).
func TestFlushFabricReportsNowExceptTheInvokingSession(t *testing.T) {
	t.Parallel()
	ch := make(chan eventReporterCall, 8)
	m, fire := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(ch))
	flushes := make(chan flushReporterCall, 8)
	m.SetFlushReporter(chanFlushReporter(flushes))

	args := defaultEventArgs()
	flushed, err := m.Subscribe(args)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	args.SessionID = 11
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe(invoking session): %v", err)
	}
	args.FabricIndex, args.SessionID = 2, 12
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe(other fabric): %v", err)
	}
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))

	ctx := context.Background()
	m.Tick(ctx, time.Now()) // inside every min interval: nothing is due
	select {
	case call := <-ch:
		t.Fatalf("a report of %d events went out on the tick", len(call.events))
	default:
	}

	m.FlushFabric(ctx, 1, 11)
	select {
	case call := <-ch:
		t.Fatalf("the flush went out through the event reporter, subscription %d", call.sub.ID)
	default:
	}
	select {
	case call := <-flushes:
		if call.sub.ID != flushed.ID || len(call.events) != 1 {
			t.Fatalf("flushed subscription %d with %d events, want %d with 1", call.sub.ID, len(call.events), flushed.ID)
		}
	default:
		t.Fatal("FlushFabric reported nothing")
	}
	select {
	case call := <-flushes:
		t.Fatalf("a second report went out, subscription %d", call.sub.ID)
	default:
	}
	m.FlushFabric(ctx, 1, 11) // nothing owed any more
	select {
	case call := <-flushes:
		t.Fatalf("a flush without pending data reported, subscription %d", call.sub.ID)
	default:
	}
}

// TestFlushFabricWaitsForTheReportInFlight: a subscription whose report
// the engine is sending at the moment of the flush is flushed once that
// report is done, not skipped — its queues were drained before the
// event arrived.
func TestFlushFabricWaitsForTheReportInFlight(t *testing.T) {
	t.Parallel()
	ch := make(chan flushReporterCall, 8)
	m, fire := newEventManager(subscription.Config{})
	m.SetFlushReporter(chanFlushReporter(ch))
	sub, err := m.Subscribe(defaultEventArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	release := subscription.HoldReporting(sub)
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))

	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		m.FlushFabric(context.Background(), 1, 0)
	}()
	select {
	case call := <-ch:
		t.Fatalf("flushed subscription %d while its report was in flight", call.sub.ID)
	case <-flushed:
		t.Fatal("FlushFabric returned while the report was in flight")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case call := <-ch:
		if call.sub.ID != sub.ID || len(call.events) != 1 {
			t.Fatalf("flushed subscription %d with %d events, want %d with 1", call.sub.ID, len(call.events), sub.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FlushFabric reported nothing after the report in flight finished")
	}
	<-flushed

	// A flush whose context ends while the report is in flight gives up.
	release = subscription.HoldReporting(sub)
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.FlushFabric(ctx, 1, 0)
	release()
	select {
	case call := <-ch:
		t.Fatalf("a flush with an ended context reported, subscription %d", call.sub.ID)
	default:
	}
}

// TestFlushFabricOneReportPerSubscription: the flush hands each
// subscription's events and dirty attributes to one flush reporter call —
// one report, as matter.js's #flush triggers one #sendUpdate carrying the
// attribute filter and the events together (ServerSubscription.ts:753-763,
// :534-556 at db021da0) — and the ongoing reporters see nothing. Without a
// flush reporter nothing is drained: what is owed stays for the next
// ongoing report (issue 473).
func TestFlushFabricOneReportPerSubscription(t *testing.T) {
	t.Parallel()
	events := make(chan eventReporterCall, 8)
	m, fire := newEventManager(subscription.Config{})
	m.SetEventReporter(chanEventReporter(events))
	attrPath := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	args := defaultEventArgs()
	args.AttributePaths = []im.ConcreteAttributePath{attrPath}
	sub, err := m.Subscribe(args)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))
	m.OnAttributeChanged(attrPath)

	m.FlushFabric(ctx, 1, 0) // no flush reporter: nothing drained
	flushes := make(chan flushReporterCall, 8)
	m.SetFlushReporter(chanFlushReporter(flushes))
	m.FlushFabric(ctx, 1, 0)
	select {
	case call := <-flushes:
		if call.sub.ID != sub.ID || len(call.events) != 1 || len(call.paths) != 1 || call.paths[0] != attrPath {
			t.Fatalf("flush of subscription %d carried %d events and paths %v, want %d with 1 event and [%v]",
				call.sub.ID, len(call.events), call.paths, sub.ID, attrPath)
		}
	default:
		t.Fatal("FlushFabric reported nothing")
	}
	select {
	case call := <-flushes:
		t.Fatalf("a second flush report went out, subscription %d", call.sub.ID)
	case call := <-events:
		t.Fatalf("the event reporter was called, subscription %d", call.sub.ID)
	default:
	}
}
