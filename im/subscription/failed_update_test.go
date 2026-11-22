// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/im"
)

// TestFailedUpdateIsOwedNotDue (review II, F-db021da0-3): the paths of a
// failed update do not ask for a report — a floor later nothing is due —
// and leave with the next report another cause starts, the keep-alive of
// the send interval here.
func TestFailedUpdateIsOwedNotDue(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_700_000_000, 0)
	m := newTestManager(Config{Clock: clocktest.New(t0)})
	sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MinIntervalFloor: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
	if err != nil {
		t.Fatal(err)
	}
	path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	sub.TouchLastReport(t0)
	sub.RequeueAttributes([]im.ConcreteAttributePath{path})
	if _, due := sub.drainDue(t0.Add(2*time.Second), nil, false); due {
		t.Fatal("a failed update's paths asked for a report a floor later")
	}
	work, due := sub.drainDue(t0.Add(sub.SendInterval()), nil, false)
	if !due || !work.keepAlive || len(work.paths) != 1 || work.paths[0] != path {
		t.Fatalf("the send interval's report = %+v (due %v), want the keep-alive with the owed path", work, due)
	}
}

// TestFlushAfterAFailedReportInFlightSendsNothing (review II,
// R2-P6-par-0702): a flush that waited for a report in flight sends
// nothing when that report failed, as matter.js's closing subscription
// does; one whose report succeeded is flushed.
func TestFlushAfterAFailedReportInFlightSendsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		for _, failed := range []bool{true, false} {
			var flushed int
			m := newTestManager(Config{Clock: clocktest.New(time.Unix(1_700_000_000, 0))})
			m.SetFlushReporter(func(context.Context, *Subscription, []im.EventReport, []im.ConcreteAttributePath) { flushed++ })
			sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
			if err != nil {
				t.Fatal(err)
			}
			path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			sub.reporting.Store(true) // a report is in flight
			done := make(chan struct{})
			go func() {
				defer close(done)
				m.FlushFabric(context.Background(), 1, 0)
			}()
			synctest.Wait()
			if failed {
				sub.RequeueAttributes([]im.ConcreteAttributePath{path})
			} else {
				sub.markDirty(path)
			}
			sub.endReport()
			<-done
			if want := map[bool]int{true: 0, false: 1}[failed]; flushed != want {
				t.Errorf("report in flight failed=%v: %d flushes, want %d", failed, flushed, want)
			}
		}
	})
}

// TestFlushAfterAnAbortedReportInFlightSends (review II, R2-P7-req-06,
// decision 274): a report in flight that was aborted, not failed, is owed
// again without counting a failed update, so a flush that waited for it
// still sends what the subscription owes — the events and the paths;
// one that failed still sends nothing.
func TestFlushAfterAnAbortedReportInFlightSends(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		for _, aborted := range []bool{true, false} {
			var flushes int
			var flushedPaths []im.ConcreteAttributePath
			m := newTestManager(Config{Clock: clocktest.New(time.Unix(1_700_000_000, 0))})
			m.SetFlushReporter(func(_ context.Context, _ *Subscription, _ []im.EventReport, paths []im.ConcreteAttributePath) { //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
				flushes++
				flushedPaths = paths
			})
			sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
			if err != nil {
				t.Fatal(err)
			}
			path := im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			sub.reporting.Store(true) // a report is in flight
			done := make(chan struct{})
			go func() {
				defer close(done)
				m.FlushFabric(context.Background(), 1, 0)
			}()
			synctest.Wait()
			if aborted {
				sub.RequeueAbortedEvents(0)
				sub.RequeueAbortedAttributes([]im.ConcreteAttributePath{path})
			} else {
				sub.RequeueEvents(0)
				sub.RequeueAttributes([]im.ConcreteAttributePath{path})
			}
			sub.endReport()
			<-done
			if want := map[bool]int{true: 1, false: 0}[aborted]; flushes != want {
				t.Errorf("report in flight aborted=%v: %d flushes, want %d", aborted, flushes, want)
			}
			if aborted && (len(flushedPaths) != 1 || flushedPaths[0] != path) {
				t.Errorf("the flush after the aborted report carried %+v, want %+v", flushedPaths, path)
			}
			if got, want := sub.failedUpdateCount(), map[bool]uint64{true: 0, false: 2}[aborted]; got != want {
				t.Errorf("aborted=%v: %d failed updates counted, want %d", aborted, got, want)
			}
		}
	})
}
