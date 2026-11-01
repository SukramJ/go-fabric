// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

// The events a subscription owes are a watermark, not a queue: matter.js
// packages/node/src/node/server/ServerSubscription.ts:391-394
// (#handleAddedEvents keeps #outstandingEventsMinNumber) and :541-555
// (#sendUpdate reads the occurrences from the log at send time).
// Review 2026-11 W2-conc-02, W2-conc-03.

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

func TestEventWatermarkParity(t *testing.T) {
	t.Parallel()
	path := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: 0x003B, Event: 0x01}
	subscribe := func(t *testing.T, floor uint16) (*Manager, *im.EventLog, *Subscription) {
		t.Helper()
		m := NewManager(Config{}, nil, nil)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 1, SessionID: 1,
			MinIntervalFloor: floor, MaxIntervalCeiling: 3600,
			EventPaths: []im.ConcreteEventPath{path},
		})
		if err != nil {
			t.Fatal(err)
		}
		return m, log, sub
	}
	emit := func(m *Manager, log *im.EventLog, payload any) uint64 {
		n := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: 0x003B, EventID: 0x01, Payload: payload})
		m.OnEventFired(EventFiring{Path: path, Number: n, Priority: im.EventPriorityInfo})
		return n
	}

	t.Run("a burst of 10000 events under a long floor holds one number, no payload", func(t *testing.T) {
		t.Parallel()
		m, log, sub := subscribe(t, 3600)
		t0 := time.Now()
		sub.TouchLastReport(t0)
		first := emit(m, log, make([]byte, 64))
		for range 9999 {
			emit(m, log, make([]byte, 64))
		}
		sub.mu.Lock()
		pending, minNumber := sub.eventsPending, sub.eventsMin
		sub.mu.Unlock()
		if !pending || minNumber != first {
			t.Fatalf("watermark = (%v, %d), want (true, %d)", pending, minNumber, first)
		}
		if got := sub.drainEventsIfElapsed(t0.Add(time.Minute), log); got != nil {
			t.Fatalf("drained %d events inside the floor", len(got))
		}
		got := sub.drainEventsIfElapsed(t0.Add(time.Hour), log)
		inLog := log.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0)
		if len(got) == 0 || len(got) != len(inLog) || len(inLog) >= 10000 {
			t.Fatalf("report carries %d events, the log %d: the log's bound must bound the report", len(got), len(inLog))
		}
	})

	t.Run("a firing that lost the race to a later one is reported, in number order, with the log's timestamp", func(t *testing.T) {
		t.Parallel()
		m, log, sub := subscribe(t, 0)
		n1 := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: 0x003B, EventID: 0x01, EpochMS: 1_000})
		n2 := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: 0x003B, EventID: 0x01, EpochMS: 2_000})
		m.OnEventFired(EventFiring{Path: path, Number: n2, Priority: im.EventPriorityInfo})
		m.OnEventFired(EventFiring{Path: path, Number: n1, Priority: im.EventPriorityInfo})
		got := sub.drainEventsIfElapsed(time.Now(), log)
		if len(got) != 2 || got[0].Number != n1 || got[1].Number != n2 {
			t.Fatalf("report = %+v, want events %d then %d", got, n1, n2)
		}
		if got[0].Timestamp != 1_000 || got[1].Timestamp != 2_000 {
			t.Errorf("timestamps = %d, %d; want the log's 1000, 2000", got[0].Timestamp, got[1].Timestamp)
		}
		// Fanned out after a drain already read it from the log: not owed again.
		m.OnEventFired(EventFiring{Path: path, Number: n2, Priority: im.EventPriorityInfo})
		if again := sub.drainEventsIfElapsed(time.Now().Add(time.Second), log); again != nil {
			t.Errorf("event %d reported twice: %+v", n2, again)
		}
	})
}
