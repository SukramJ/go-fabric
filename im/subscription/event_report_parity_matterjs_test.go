// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

// How a subscription reports events, held against matter.js
// packages/node/src/node/server/ServerSubscription.ts at db021da0 (review
// II, W2-conc-05 and W2-conc-04). matter.js has no test of its own for
// these clauses at that commit (ServerSubscriptionTest.ts emits no event),
// so each case names the source lines it holds:
//
//   - :401-409 #sendEventUrgently — urgency is the isUrgent flag of a
//     request path that covers the event, wildcards included; the event's
//     priority is not read.
//   - :385-399 #handleAddedEvents — every event is owed (the lowest
//     outstanding number); only an urgent one calls #prepareDataUpdate.
//   - :454-478 #prepareDataUpdate — an update waits for
//     max(lastUpdateTime + minIntervalFloor, deferredUntil).
//   - :480-485 #startSendInterval — the send interval's pass takes what is
//     owed.
//   - :539-555 #sendUpdate — the changed attributes and the owed events
//     leave in one report; :569-584 a failed one owes both again.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// reportCall is one report the engine handed to a reporter hook.
type reportCall struct {
	at     time.Duration
	hook   string // "event" or "attribute"
	events []uint64
	paths  []im.ConcreteAttributePath
}

// eventReportBench is a manager with an event log, one subscription and
// the reports it was handed, each stamped with the tick that produced it.
type eventReportBench struct {
	t     *testing.T
	m     *subscription.Manager
	log   *im.EventLog
	sub   *subscription.Subscription
	start time.Time

	mu      sync.Mutex
	now     time.Duration
	reports []reportCall
	// filtered makes the event reporter find nothing left to send for the
	// subscriber, as the bridge's does when its access check drops every
	// event: it sends nothing and says so.
	filtered bool
}

// newEventReportBench subscribes with args. takePaths makes the event
// reporter take the attribute paths due with its events, as the bridge's
// does; without it the reporter is one written before the fold.
func newEventReportBench(t *testing.T, args subscription.SubscribeArgs, takePaths bool) *eventReportBench {
	t.Helper()
	b := &eventReportBench{t: t, log: im.NewEventLog(), start: time.Unix(1_800_000_000, 0)}
	b.m = subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.reports = append(b.reports, reportCall{at: b.now, hook: "attribute", paths: paths})
	}, nil)
	b.m.SetEventSource(b.log)
	b.m.SetEventReporter(func(_ context.Context, sub *subscription.Subscription, events []im.EventReport) {
		call := reportCall{hook: "event"}
		for _, ev := range events {
			call.events = append(call.events, ev.Number)
		}
		if takePaths {
			call.paths = sub.TakeDuePaths()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		call.at = b.now
		if b.filtered {
			call.hook = "event, nothing sent"
			sub.NothingSent()
		}
		b.reports = append(b.reports, call)
	})
	sub, err := b.m.Subscribe(args)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b.sub = sub
	sub.TouchLastReport(b.start)
	return b
}

// emit appends an event to the log and fans it out, as the bridge does.
func (b *eventReportBench) emit(endpoint uint16, cluster, event uint32, priority im.EventPriority) uint64 {
	n := b.log.Append(im.EventRecord{Priority: priority, Endpoint: endpoint, Cluster: cluster, EventID: event, Payload: uint8(1)})
	b.m.OnEventFired(subscription.EventFiring{Path: mkEventPath(endpoint, cluster, event), Number: n, Priority: priority})
	return n
}

// tick runs the engine at start+at.
func (b *eventReportBench) tick(at time.Duration) {
	b.mu.Lock()
	b.now = at
	b.mu.Unlock()
	b.m.Tick(context.Background(), b.start.Add(at))
}

// taken returns the reports handed out so far and forgets them.
func (b *eventReportBench) taken() []reportCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.reports
	b.reports = nil
	return out
}

// wantNone fails when a report was handed out.
func (b *eventReportBench) wantNone(why string) {
	b.t.Helper()
	if got := b.taken(); len(got) != 0 {
		b.t.Fatalf("%s: got %+v, want no report", why, got)
	}
}

// wantOne returns the one report handed out, failing on any other count.
func (b *eventReportBench) wantOne(why string) reportCall {
	b.t.Helper()
	got := b.taken()
	if len(got) != 1 {
		b.t.Fatalf("%s: got %d reports %+v, want 1", why, len(got), got)
	}
	return got[0]
}

func equalNumbers(got []uint64, want ...uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// eventArgs is a subscription with a 10 s floor and a 60 s ceiling (send
// interval 48 s) on the given event paths and, when attr is set, the OnOff
// attribute of endpoint 1.
func eventArgs(attr bool, paths ...im.ConcreteEventPath) subscription.SubscribeArgs {
	args := subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0xBEEF, SessionID: 10,
		MinIntervalFloor: 10, MaxIntervalCeiling: 60,
		EventPaths: paths,
	}
	if attr {
		args.AttributePaths = []im.ConcreteAttributePath{mkPath(1, 0x0006, 0x0000)}
	}
	return args
}

func urgent(p im.ConcreteEventPath) im.ConcreteEventPath {
	p.IsUrgent = true
	return p
}

const (
	switchCluster = 0x003B
	initialPress  = 0x01
)

func TestEventReportParityMatterJS(t *testing.T) {
	t.Parallel()
	press := mkEventPath(1, switchCluster, initialPress)

	// :401-409 — isUrgent is the only thing read; :385-399 reads no priority.
	t.Run("a critical event on a path that is not urgent schedules no report", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, eventArgs(false, press), true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityCritical)
		b.tick(time.Second)
		b.wantNone("inside the floor")
		b.tick(11 * time.Second)
		b.wantNone("past the floor: a non-urgent event asks for no report")
		b.tick(47 * time.Second)
		b.wantNone("before the send interval")
		// :480-485 — the send interval's pass takes it.
		b.tick(48 * time.Second)
		got := b.wantOne("at the send interval")
		if got.hook != "event" || !equalNumbers(got.events, n) {
			t.Fatalf("the send interval's report = %+v, want event %d", got, n)
		}
		b.tick(48*time.Second + 250*time.Millisecond)
		b.wantNone("the event report is the report of that interval, no keep-alive follows it")
	})

	// :454-478 — the update an urgent event asks for waits for the floor.
	for _, priority := range []im.EventPriority{im.EventPriorityInfo, im.EventPriorityCritical} {
		name := map[im.EventPriority]string{im.EventPriorityInfo: "info", im.EventPriorityCritical: "critical"}[priority]
		t.Run("an urgent path's "+name+" event is reported when the floor ends, not before", func(t *testing.T) {
			t.Parallel()
			b := newEventReportBench(t, eventArgs(false, urgent(press)), true)
			n := b.emit(1, switchCluster, initialPress, priority)
			b.tick(200 * time.Millisecond)
			b.wantNone("200 ms after the last report")
			b.tick(10*time.Second - 250*time.Millisecond)
			b.wantNone("a tick before the floor ends")
			b.tick(10 * time.Second)
			got := b.wantOne("when the floor ends")
			if got.hook != "event" || !equalNumbers(got.events, n) {
				t.Fatalf("report = %+v, want event %d", got, n)
			}
		})
	}

	t.Run("an urgent event past the floor is reported on the next tick", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, eventArgs(false, urgent(press)), true)
		b.tick(20 * time.Second)
		b.wantNone("nothing owed")
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.tick(20*time.Second + 250*time.Millisecond)
		if got := b.wantOne("the tick after the event"); !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want event %d", got, n)
		}
	})

	// :405-407 — an undefined field of the request path covers every value.
	t.Run("a wildcard urgent path makes every event it covers urgent", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, eventArgs(false, im.ConcreteEventPath{IsUrgent: true}), true)
		n := b.emit(7, 0x0045, 0x00, im.EventPriorityInfo)
		b.tick(10 * time.Second)
		if got := b.wantOne("when the floor ends"); !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want event %d", got, n)
		}
	})

	// :402 `some` — one urgent path that covers the event is enough, and a
	// path that does not cover it lends it no urgency.
	t.Run("urgency is per path", func(t *testing.T) {
		t.Parallel()
		other := mkEventPath(2, 0x0045, 0x00)
		b := newEventReportBench(t, eventArgs(false, press, urgent(other)), true)
		quiet := b.emit(1, switchCluster, initialPress, im.EventPriorityCritical)
		b.tick(11 * time.Second)
		b.wantNone("the urgent path does not cover this event")
		// :542, :876 — the report reads from the lowest outstanding number,
		// so the urgent event takes the one owed before it along.
		loud := b.emit(2, 0x0045, 0x00, im.EventPriorityInfo)
		b.tick(11*time.Second + 250*time.Millisecond)
		if got := b.wantOne("the urgent event's report"); !equalNumbers(got.events, quiet, loud) {
			t.Fatalf("report = %+v, want events %d and %d", got, quiet, loud)
		}
	})

	// :466 — the deferral is part of `earliest` for an urgent event too.
	t.Run("an urgent critical event waits for a DelayReportData deferral", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, eventArgs(false, urgent(press)), true)
		b.sub.DeferReports(b.start.Add(10*time.Second), 5*time.Second)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityCritical)
		b.tick(11 * time.Second)
		b.wantNone("past the floor, inside the deferral")
		b.tick(15 * time.Second)
		if got := b.wantOne("when the deferral ends"); !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want event %d", got, n)
		}
	})

	// :539-555 — one #sendUpdate takes the attribute filter and the events'
	// minimum number; an attribute change is what starts it here (:380-382).
	t.Run("a non-urgent event leaves with the next attribute report, in the same report", func(t *testing.T) {
		t.Parallel()
		attr := mkPath(1, 0x0006, 0x0000)
		b := newEventReportBench(t, eventArgs(true, press), true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.tick(11 * time.Second)
		b.wantNone("past the floor, nothing but a non-urgent event owed")
		b.m.OnAttributeChanged(attr)
		b.tick(12 * time.Second)
		got := b.wantOne("the attribute change's report")
		if !equalNumbers(got.events, n) || len(got.paths) != 1 || got.paths[0] != attr {
			t.Fatalf("report = %+v, want event %d and path %+v together", got, n, attr)
		}
		b.tick(12*time.Second + 250*time.Millisecond)
		b.wantNone("nothing is left for a second report")
	})

	t.Run("an urgent event and an attribute change due together are one report", func(t *testing.T) {
		t.Parallel()
		attr := mkPath(1, 0x0006, 0x0000)
		b := newEventReportBench(t, eventArgs(true, urgent(press)), true)
		b.m.OnAttributeChanged(attr)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityCritical)
		b.tick(time.Second)
		b.wantNone("inside the floor neither leaves")
		b.tick(10 * time.Second)
		got := b.wantOne("when the floor ends")
		if got.hook != "event" || !equalNumbers(got.events, n) || len(got.paths) != 1 || got.paths[0] != attr {
			t.Fatalf("report = %+v, want event %d and path %+v together", got, n, attr)
		}
		b.tick(20 * time.Second)
		b.wantNone("the attribute went out with the event, not again a floor later")
	})

	// An event reporter that does not take the paths keeps what it did
	// before the fold: the attribute change follows in a report of its own,
	// a floor later, and is not lost.
	t.Run("an event reporter that takes no paths leaves them to the next attribute report", func(t *testing.T) {
		t.Parallel()
		attr := mkPath(1, 0x0006, 0x0000)
		b := newEventReportBench(t, eventArgs(true, urgent(press)), false)
		b.m.OnAttributeChanged(attr)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.tick(10 * time.Second)
		if got := b.wantOne("when the floor ends"); got.hook != "event" || !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want the event report alone", got)
		}
		b.tick(19 * time.Second)
		b.wantNone("inside the floor after the event report")
		b.tick(20 * time.Second)
		if got := b.wantOne("a floor later"); got.hook != "attribute" || len(got.paths) != 1 || got.paths[0] != attr {
			t.Fatalf("report = %+v, want the attribute path", got)
		}
	})

	// :582-584 puts the number back. The retry here goes out a floor after
	// the failed report, as a failed attribute report's does; matter.js
	// schedules nothing there.
	t.Run("events owed again after a failed report are retried when the floor ends", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, eventArgs(false, press), true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.tick(48 * time.Second)
		b.wantOne("the send interval's report, which fails")
		b.sub.RequeueEvents(n)
		b.tick(57 * time.Second)
		b.wantNone("inside the floor after the failed report")
		b.tick(58 * time.Second)
		if got := b.wantOne("a floor after the failed report"); !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want event %d again", got, n)
		}
	})

	// The events owed when the priming report ends leave with the first
	// report after the floor, as the attribute changes of that window do.
	t.Run("an event fired while the subscription was priming is reported after the floor", func(t *testing.T) {
		t.Parallel()
		args := eventArgs(false, press)
		args.Priming = true
		b := newEventReportBench(t, args, true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.sub.EndPriming(b.start)
		b.tick(time.Second)
		b.wantNone("inside the floor")
		b.tick(10 * time.Second)
		if got := b.wantOne("when the floor ends"); !equalNumbers(got.events, n) {
			t.Fatalf("report = %+v, want event %d", got, n)
		}
	})
}
