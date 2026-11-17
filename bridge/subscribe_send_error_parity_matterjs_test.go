// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A failed ongoing report owes the subscriber the same data again: matter.js
// packages/node/src/node/server/ServerSubscription.ts:566-581 puts the
// attribute filter back and restores #outstandingEventsMinNumber while
// #sendUpdateErrorCounter <= 2 (review 2026-11 W2-conc-01). A report that
// carried both owes both again (review II, W2-conc-04).

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

func TestFailedReportRetriesTheSameDataParity(t *testing.T) {
	t.Parallel()
	attrPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	// The subscriber's isUrgent flag: its events ask for a report.
	eventPath := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: 0x003B, Event: 0x01, IsUrgent: true}

	type rig struct {
		b      *Bridge
		m      *subscription.Manager
		sub    *subscription.Subscription
		mu     sync.Mutex
		paths  [][]im.ConcreteAttributePath
		events [][]im.EventReport
	}
	setup := func(t *testing.T, sessionID uint16) *rig {
		t.Helper()
		r := &rig{b: newStartedBridge(t)}
		_, _, peerAddr := chunkTestPeer(t, r.b, sessionID)
		r.m = subscription.NewManager(subscription.Config{}, func(ctx context.Context, sub *subscription.Subscription, paths []im.ConcreteAttributePath) {
			r.mu.Lock()
			r.paths = append(r.paths, paths)
			r.mu.Unlock()
			r.b.reportSubscription(ctx, sub, paths)
		}, nil)
		r.m.SetEventReporter(func(ctx context.Context, sub *subscription.Subscription, events []im.EventReport) { //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
			r.mu.Lock()
			r.events = append(r.events, events)
			r.mu.Unlock()
			r.b.reportSubscriptionEvents(ctx, sub, events)
		})
		r.b.AttachSubscriptionManager(r.m)
		sub, err := r.m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0xBBBB4444, SessionID: sessionID,
			MaxIntervalCeiling: 60,
			AttributePaths:     []im.ConcreteAttributePath{attrPath},
			EventPaths:         []im.ConcreteEventPath{eventPath},
		})
		if err != nil {
			t.Fatal(err)
		}
		r.sub = sub
		// Routed to a session the lookup does not know: every send fails
		// before a datagram leaves.
		r.b.routing.subTargets.Store(sub.ID, subTarget{
			src: peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
			exchangeID: 21, sessionID: sessionID + 100, peerInitiator: true, pase: true,
		})
		return r
	}

	t.Run("attribute paths", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 75)
		t0 := time.Now()
		r.m.OnAttributeChanged(attrPath)
		r.m.Tick(context.Background(), t0)
		r.m.Tick(context.Background(), t0.Add(r.sub.SendInterval())) // the next report: the send interval's (F-db021da0-3)
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.paths) != 2 {
			t.Fatalf("reporter called %d times, want 2: the failed report's paths are not retried", len(r.paths))
		}
		if len(r.paths[1]) != 1 || r.paths[1][0] != attrPath {
			t.Errorf("retry reported %v, want [%v]", r.paths[1], attrPath)
		}
	})

	t.Run("events", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 76)
		t0 := time.Now()
		r.b.EmitEvent(1, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityInfo))
		r.m.Tick(context.Background(), t0)
		r.m.Tick(context.Background(), t0.Add(r.sub.SendInterval())) // the next report: the send interval's (F-db021da0-3)
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.events) != 2 {
			t.Fatalf("event reporter called %d times, want 2: the failed report's events are not retried", len(r.events))
		}
		if len(r.events[1]) != 1 || r.events[1][0].Number != r.events[0][0].Number {
			t.Errorf("retry reported %+v, want event %d again", r.events[1], r.events[0][0].Number)
		}
	})

	t.Run("an event and an attribute path in one report", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 77)
		t0 := time.Now()
		r.m.OnAttributeChanged(attrPath)
		r.b.EmitEvent(1, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityInfo))
		r.m.Tick(context.Background(), t0)
		// The failed report took the path with its event; both are owed
		// again and leave together in the retry.
		r.m.Tick(context.Background(), t0.Add(r.sub.SendInterval())) // the next report: the send interval's (F-db021da0-3)
		// What the failed retry owes again, read through a flush.
		var owedEvents []im.EventReport
		var owedPaths []im.ConcreteAttributePath
		r.m.SetFlushReporter(func(_ context.Context, _ *subscription.Subscription, events []im.EventReport, paths []im.ConcreteAttributePath) { //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
			owedEvents, owedPaths = events, paths
		})
		r.m.FlushFabric(context.Background(), 1, 0)
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(owedEvents) != 1 || len(owedPaths) != 1 || owedPaths[0] != attrPath {
			t.Fatalf("owed after the failed retry: events %+v, paths %+v; want the event and %+v", owedEvents, owedPaths, attrPath)
		}
		if len(r.events) != 2 || len(r.paths) != 0 {
			t.Fatalf("event reporter called %d times, attribute reporter %d times; want 2 and 0: one report carries both, and so does its retry", len(r.events), len(r.paths))
		}
		if len(r.events[1]) != 1 || r.events[1][0].Number != r.events[0][0].Number {
			t.Errorf("retry reported %+v, want event %d again", r.events[1], r.events[0][0].Number)
		}
		if r.sub.TakeDuePaths() != nil { //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
			t.Error("paths left on the subscription after the reports")
		}
	})
}

// undeliveredReportRig is a device with a subscription whose peer never
// acknowledges a report: two sessions of the subscriber, the
// subscription on the first.
type undeliveredReportRig struct {
	dev        *resumeDevice
	outbound   *outboundReliableTracker
	sub        *subscription.Subscription
	reportSID  uint16
	siblingSID uint16
	graceful   *atomic.Int32
}

func newUndeliveredReportRig(t *testing.T, base uint16) *undeliveredReportRig {
	t.Helper()
	stamp := &stampClock{}
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		onStampClock(br, sessions, stamp)
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	r := &undeliveredReportRig{dev: dev, reportSID: base, siblingSID: base + 1, graceful: &atomic.Int32{}}
	dev.sessions.SetGracefulCloseNotifier(func(uint16, *channel.Session) { r.graceful.Add(1) })
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	peerAddr, ok := silent.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", silent.LocalAddr())
	}
	for _, sid := range []uint16{r.reportSID, r.siblingSID} {
		if _, err := dev.sessions.OpenFromSigmaWithID(sid, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, sid+0x100, nil, peerLossKeys(t)); err != nil { //nolint:staticcheck // SA1019: a fixture session under a chosen id, which no stake install takes (N3-findev-1)
			t.Fatal(err)
		}
		dev.br.sessionPeerAddrs.Store(sid, peerAddr)
	}
	r.sub, err = dev.subs.Subscribe(subscription.SubscribeArgs{
		FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode, SessionID: r.reportSID,
		MaxIntervalCeiling: 3600, KeepSubscriptions: true,
		AttributePaths: []im.ConcreteAttributePath{undeliveredReportPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.br.routing.subTargets.Store(r.sub.ID, subTarget{
		src: peerAddr, sessionID: r.reportSID, fabricIndex: resumeFabricIndex, subjectNodeID: resumeCtrlNode,
	})
	dev.br.mu.RLock()
	r.outbound = dev.br.outboundReliable
	dev.br.mu.RUnlock()
	if r.outbound == nil {
		t.Fatal("no outbound-reliable tracker")
	}
	return r
}

var undeliveredReportPath = im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x001F, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}

// report starts one ongoing report of the subscription's path. Its
// reporter waits for the update to settle ([Bridge.deliverOngoingReport]),
// so it runs on a goroutine of its own, its wait ended with the test.
func (r *undeliveredReportRig) report(t *testing.T) {
	t.Helper()
	r.reportPaths(t, []im.ConcreteAttributePath{undeliveredReportPath})
}

// reportPaths is [undeliveredReportRig.report] for paths (nil: the
// keep-alive); done closes when the reporter returned.
func (r *undeliveredReportRig) reportPaths(t *testing.T, paths []im.ConcreteAttributePath) (done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	t.Cleanup(func() { cancel(); <-ended })
	go func() {
		defer close(ended)
		r.dev.br.reportSubscription(ctx, r.sub, paths)
	}()
	return ended
}

// answer answers the report that waits for its StatusResponse on the
// subscription's session with status, as the inbound StatusResponse does
// ([Bridge.absorbStatusResponse] → signalStatusResponseRX).
func (r *undeliveredReportRig) answer(t *testing.T, status im.StatusCode) {
	t.Helper()
	var key mrp.ExchangeKey
	waitFor(t, "a report waiting for its StatusResponse", func() bool {
		found := false
		r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			if ek, ok := k.(mrp.ExchangeKey); ok && ek.SessionID == r.reportSID && ek.Initiator {
				key, found = ek, true
				return false
			}
			return true
		})
		return found
	})
	if !r.dev.br.signalStatusResponseRX(key.SessionID, key.ExchangeID, true, status) {
		t.Fatal("the StatusResponse found no waiting report")
	}
}

// answerChecked is [undeliveredReportRig.answer] that runs before once the
// report is seen waiting and before the answer goes in.
func (r *undeliveredReportRig) answerChecked(t *testing.T, status im.StatusCode, before func()) {
	t.Helper()
	waitFor(t, "a report waiting for its StatusResponse", func() bool {
		found := false
		r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			ek, ok := k.(mrp.ExchangeKey)
			found = ok && ek.SessionID == r.reportSID && ek.Initiator
			return !found
		})
		return found
	})
	before()
	r.answer(t, status)
}

// pending waits for the subscription's next report to be tracked for
// retransmission and returns the counter of its message. After a failed
// update it is the device's subscription engine that sends it, with what
// the failed report owed: nothing else is due (no change, the keep-alive
// half an hour away).
func (r *undeliveredReportRig) pending(t *testing.T) uint32 {
	t.Helper()
	var counter uint32
	waitFor(t, "a report tracked for retransmission", func() bool {
		r.outbound.mu.Lock()
		defer r.outbound.mu.Unlock()
		for key := range r.outbound.pending {
			if key.sessionID == r.reportSID {
				counter = key.counter
				return true
			}
		}
		return false
	})
	return counter
}

// exhaust drives every tracked message past its last retransmission.
func (r *undeliveredReportRig) exhaust() {
	now := time.Now()
	for i := 0; i <= mrp.MaxRetransmissions+1; i++ {
		r.dev.br.tickOutboundReliable(r.outbound, now.Add(time.Duration(i+1)*time.Hour))
	}
}

// sessionsKept fails the test when a session of the subscriber was closed
// or a CloseSession was sent.
func (r *undeliveredReportRig) sessionsKept(t *testing.T, when string) {
	t.Helper()
	for _, sid := range []uint16{r.reportSID, r.siblingSID} {
		if _, err := r.dev.sessions.Get(sid); err != nil {
			t.Errorf("%s: session %#x closed: %v", when, sid, err)
		}
	}
	if n := r.graceful.Load(); n != 0 {
		t.Errorf("%s: CloseSession sent %d times", when, n)
	}
}

// TestUndeliveredReportIsOneFailedUpdateParity ports matter.js
// packages/node/test/node/ServerSubscriptionTest.ts "abandons the
// subscription after repeated MRP exhaustion, leaving the session alone"
// at db021da0: a report whose message runs out of retransmissions is one
// failed update — its data is owed again, and the subscription ends after
// the third (ServerSubscription.ts:563-603). It is no peer loss: report
// exchanges are opened with suppressPeerLoss (InteractionServer.ts:96-97,
// :785-797), so every session of the subscriber stays and no CloseSession
// is sent.
func TestUndeliveredReportIsOneFailedUpdateParity(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1401)
	r.report(t)
	for failed := 1; failed <= 3; failed++ {
		if failed > 1 {
			// The failed update's data is owed, not due: a change starts
			// the next update, which carries it (F-db021da0-3).
			r.dev.subs.OnAttributeChanged(undeliveredReportPath)
		}
		r.pending(t)
		r.exhaust()
		r.sessionsKept(t, fmt.Sprintf("after %d failed updates", failed))
		_, err := r.dev.subs.Get(r.sub.ID)
		if failed < 3 && err != nil {
			t.Fatalf("subscription ended after %d failed updates, want after 3: %v", failed, err)
		}
		if failed == 3 && err == nil {
			t.Fatal("subscription still open after 3 failed updates")
		}
	}
}

// TestDeliveredReportRestartsTheFailedUpdateCount: a report the peer
// answers with a Success StatusResponse is a delivered update, and the
// count of failed ones starts again. matter.js ServerSubscription.ts:555-557
// at db021da0 resets #sendUpdateErrorCounter when #sendUpdateMessage
// returns, which is after the peer's StatusResponse to the report
// (packages/protocol/src/interaction/InteractionMessenger.ts:838-845,
// waitForSuccess at :844). The report's acknowledgement alone delivers
// nothing (F-db021da0-36).
func TestDeliveredReportRestartsTheFailedUpdateCount(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1501)
	r.report(t)
	for range 2 {
		r.pending(t)
		r.exhaust()
		// The failed update's data is owed, not due: a change starts
		// the next update (F-db021da0-3).
		r.dev.subs.OnAttributeChanged(undeliveredReportPath)
	}
	counter := r.pending(t)
	if !r.outbound.Ack(r.reportSID, counter) {
		t.Fatal("the report's acknowledgement found no entry")
	}
	r.dev.br.releaseReportCounter(r.reportSID, counter)
	if raw, _ := r.dev.br.subSendErrorCount.Load(r.sub.ID); raw != 2 {
		t.Fatalf("failed updates after the acknowledgement: %v, want still 2 — an acknowledgement delivers nothing", raw)
	}
	r.answer(t, im.StatusSuccess)
	waitFor(t, "the delivered update", func() bool {
		_, counted := r.dev.br.subSendErrorCount.Load(r.sub.ID)
		return !counted
	})
	r.report(t)
	for i := range 2 {
		r.pending(t)
		r.exhaust()
		if i == 0 {
			r.dev.subs.OnAttributeChanged(undeliveredReportPath)
		}
	}
	if _, err := r.dev.subs.Get(r.sub.ID); err != nil {
		t.Fatalf("subscription ended after two failed updates following a delivered one: %v", err)
	}
	r.sessionsKept(t, "after a delivered and four failed updates")
}

// inFlight counts the subscription's report messages that wait for the
// peer's acknowledgement.
func (r *undeliveredReportRig) inFlight() int {
	r.outbound.mu.Lock()
	defer r.outbound.mu.Unlock()
	n := 0
	for key := range r.outbound.pending {
		if key.sessionID == r.reportSID {
			n++
		}
	}
	return n
}

// TestOneReportInFlightUntilItsStatusResponseParity: a subscription has one
// report in flight until the peer's StatusResponse settles it — an
// acknowledged report still waits — and the changes made meanwhile leave
// in one next report once it is answered. matter.js sends no update while
// one is in flight (packages/node/src/node/server/ServerSubscription.ts:520-529
// at db021da0), an update lasts until sendDataReport's waitForSuccess
// (packages/protocol/src/interaction/InteractionMessenger.ts:838-845), and
// what changed meanwhile is sent as one next report (ServerSubscription.ts
// :346-357, :537-543, :610-622).
func TestOneReportInFlightUntilItsStatusResponseParity(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1601)
	r.dev.subs.OnAttributeChanged(undeliveredReportPath)
	first := r.pending(t)
	if !r.outbound.Ack(r.reportSID, first) {
		t.Fatal("the report's acknowledgement found no entry")
	}
	r.dev.br.releaseReportCounter(r.reportSID, first)
	for range 3 {
		r.dev.subs.OnAttributeChanged(undeliveredReportPath)
	}
	// Ticks well past the subscription's floor, with the first report
	// acknowledged and unanswered: a tick starts no report of a
	// subscription whose report is in flight.
	now := time.Now()
	for i := range 3 {
		r.dev.subs.Tick(context.Background(), now.Add(time.Duration(i+1)*10*time.Second))
	}
	if n := r.inFlight(); n != 0 {
		t.Fatalf("%d reports sent while the first waits for its StatusResponse, want none", n)
	}
	r.answer(t, im.StatusSuccess)
	if second := r.pending(t); second == first {
		t.Fatal("no next report after the StatusResponse")
	}
	r.dev.subs.Tick(context.Background(), now.Add(time.Minute))
	if n := r.inFlight(); n != 1 {
		t.Fatalf("%d reports after the answer, want the changes in one", n)
	}
	r.sessionsKept(t, "after a delivered update")
}

// TestRefusedReportEndsTheSubscriptionParity: a report the peer answers
// with an error status ends the subscription at once, without a retry,
// and leaves its sessions. matter.js ServerSubscription.ts:938-950 at
// db021da0: every error status marks the subscription terminated and
// #closeFromUpdate cancels it.
func TestRefusedReportEndsTheSubscriptionParity(t *testing.T) {
	t.Parallel()
	for _, status := range []im.StatusCode{im.StatusInvalidSubscription, im.StatusFailure, im.StatusBusy} {
		t.Run(status.String(), func(t *testing.T) {
			t.Parallel()
			r := newUndeliveredReportRig(t, 0x1701)
			r.report(t)
			r.pending(t)
			r.answer(t, status)
			waitFor(t, "the subscription's end", func() bool {
				_, err := r.dev.subs.Get(r.sub.ID)
				return err != nil
			})
			if _, counted := r.dev.br.subSendErrorCount.Load(r.sub.ID); counted {
				t.Error("a refused report counted as a failed update")
			}
			r.sessionsKept(t, "after the refusal")
		})
	}
}

// TestKeepAliveWaitsForItsStatusResponseParity: the keep-alive, an empty
// report, waits for the peer's StatusResponse as a report with data does
// — its reporter returns only once it is answered. matter.js sends it with
// `suppressResponse: false` (ServerSubscription.ts:913-914 at db021da0)
// through the same sendDataReport and its waitForSuccess
// (InteractionMessenger.ts:838-845).
func TestKeepAliveWaitsForItsStatusResponseParity(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1801)
	done := r.reportPaths(t, nil)
	counter := r.pending(t)
	if !r.outbound.Ack(r.reportSID, counter) {
		t.Fatal("the keep-alive's acknowledgement found no entry")
	}
	r.dev.br.releaseReportCounter(r.reportSID, counter)
	// The keep-alive's reporter waits for its StatusResponse (answer
	// finds the wait) and has not returned on the acknowledgement.
	r.answerChecked(t, im.StatusSuccess, func() {
		select {
		case <-done:
			t.Fatal("the keep-alive's reporter returned before its StatusResponse")
		default:
		}
	})
	<-done // a reporter that never returns fails the test by its timeout
}
