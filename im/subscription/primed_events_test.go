// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// TestPrimedEventsAreNotReportedAgain (review II, R2-P1-req2-03): an event
// that fires while a subscription is priming, before its priming report
// read the log, is in the priming report; once the report names the last
// event it carried, the first report after the priming does not carry it
// again — only what fired after it. matter.js corrects the outstanding
// number past the latest seeded event at activation, or clears it ("We
// already sent out the latest event number"), ServerSubscription.ts:431-437
// at db021da0.
func TestPrimedEventsAreNotReportedAgain(t *testing.T) {
	t.Parallel()
	path := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: 0x003B, Event: 0x01}
	setup := func(t *testing.T) (*Manager, *im.EventLog, *Subscription) {
		t.Helper()
		m := NewManager(Config{}, nil, nil)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 3600,
			EventPaths: []im.ConcreteEventPath{path}, Priming: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return m, log, sub
	}
	emit := func(m *Manager, log *im.EventLog) uint64 {
		n := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: 0x003B, EventID: 0x01})
		m.OnEventFired(EventFiring{Path: path, Number: n, Priority: im.EventPriorityInfo})
		return n
	}
	numbers := func(work dueWork) []uint64 {
		out := make([]uint64, 0, len(work.events))
		for _, ev := range work.events {
			out = append(out, ev.Number)
		}
		return out
	}

	t.Run("the primed event only", func(t *testing.T) {
		t.Parallel()
		m, log, sub := setup(t)
		primed := emit(m, log)
		sub.PrimedEvents(primed)
		t0 := time.Now()
		sub.EndPriming(t0)
		if work, _ := sub.drainDue(t0.Add(time.Hour), log, false); len(work.events) != 0 {
			t.Fatalf("the first report after the priming carries events %v; event %d was in the priming report", numbers(work), primed)
		}
	})
	t.Run("an event after the primed one", func(t *testing.T) {
		t.Parallel()
		m, log, sub := setup(t)
		primed := emit(m, log)
		later := emit(m, log)
		sub.PrimedEvents(primed)
		t0 := time.Now()
		sub.EndPriming(t0)
		work, _ := sub.drainDue(t0.Add(time.Hour), log, false)
		if got := numbers(work); len(got) != 1 || got[0] != later {
			t.Fatalf("the first report after the priming carries events %v, want only %d", got, later)
		}
	})
}
