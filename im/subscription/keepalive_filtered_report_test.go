// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

// The keep-alive and a report that sends nothing (review II,
// R2-P0-rv-A03). A report whose events the reporter filters down to
// nothing for the subscriber — the bridge's access check, a
// fabric-sensitive event of another fabric — is not sent; the reporter
// says so ([subscription.Subscription.NothingSent]). The engine then sends
// the keep-alive that was due in the same pass, and does not count a pass
// without one as the subscription's last report.
//
// matter.js at db021da0 has no such case to hold these against: its pass
// sends the DataReport empty and counts it
// (packages/node/src/node/server/ServerSubscription.ts:549-551 stamps
// #lastUpdateTime before the send, :921-936 sends with
// `suppressEmptyReport: onlyWithData`, and
// packages/protocol/src/interaction/InteractionMessenger.ts:794-796 sends
// an empty report unless that flag is set), under a TODO of its own
// (ServerSubscription.ts:923-925). What the two share is the clause these
// tests are about: the send interval's report leaves at the send interval
// (:480-485), whatever was owed.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// firstReports ticks the bench once a second up to until and returns the
// reports it was handed, in order.
func firstReports(b *eventReportBench, until time.Duration) []reportCall {
	var out []reportCall
	for at := time.Second; at <= until; at += time.Second {
		b.tick(at)
		out = append(out, b.taken()...)
	}
	return out
}

func TestKeepAliveAndAReportThatSendsNothing(t *testing.T) {
	t.Parallel()
	press := mkEventPath(1, switchCluster, initialPress)
	// A 1 s floor and a 60 s ceiling: send interval 48 s.
	args := func(paths ...im.ConcreteEventPath) subscription.SubscribeArgs {
		a := eventArgs(false, paths...)
		a.MinIntervalFloor = 1
		return a
	}
	isKeepAlive := func(c reportCall) bool { return c.hook == "attribute" && len(c.paths) == 0 }

	// The review's scenario: a max interval of 60 s, a non-urgent event
	// owed that the subscriber may not read. Before, the first thing the
	// subscriber heard was the keep-alive at 96 s.
	t.Run("a filtered non-urgent event owed: the keep-alive leaves at the send interval", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, args(im.ConcreteEventPath{}), true)
		b.filtered = true
		if got := b.sub.SendInterval(); got != 48*time.Second {
			t.Fatalf("send interval = %v, want 48s", got)
		}
		b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		got := firstReports(b, 96*time.Second)
		// The event reporter is asked once and sends nothing; the
		// keep-alive follows in the same pass, and the next one a send
		// interval later.
		if len(got) != 3 || got[0].hook != "event, nothing sent" || got[0].at != 48*time.Second ||
			!isKeepAlive(got[1]) || got[1].at != 48*time.Second ||
			!isKeepAlive(got[2]) || got[2].at != 96*time.Second {
			t.Fatalf("reports = %+v, want the filtered event report and the keep-alive at 48s, the next keep-alive at 96s", got)
		}
	})

	t.Run("a filtered urgent event's report is no report: the keep-alive stays where it was", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, args(urgent(press)), true)
		b.filtered = true
		b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		got := firstReports(b, 50*time.Second)
		// Counted, the pass at 1 s put the keep-alive at 49 s.
		if len(got) != 2 || got[0].hook != "event, nothing sent" || got[0].at != time.Second ||
			!isKeepAlive(got[1]) || got[1].at != 48*time.Second {
			t.Fatalf("reports = %+v, want the filtered event report at 1s and the keep-alive at 48s", got)
		}
	})

	t.Run("a filtered urgent event's report does not hold an attribute change back", func(t *testing.T) {
		t.Parallel()
		a := eventArgs(true, urgent(press)) // floor 10 s
		b := newEventReportBench(t, a, true)
		b.filtered = true
		b.tick(15 * time.Second)
		b.wantNone("nothing owed")
		b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		b.tick(16 * time.Second)
		if got := b.wantOne("the urgent event's pass"); got.hook != "event, nothing sent" {
			t.Fatalf("report = %+v, want the event reporter asked", got)
		}
		b.filtered = false
		b.m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
		// Counted, the pass at 16 s held the change until 26 s.
		b.tick(17 * time.Second)
		if got := b.wantOne("the attribute change, a second later"); got.hook != "attribute" || len(got.paths) != 1 {
			t.Fatalf("report = %+v, want the changed path", got)
		}
	})

	t.Run("without an event reporter an urgent event's pass is no report either", func(t *testing.T) {
		t.Parallel()
		keepAlives := []time.Duration{}
		var now time.Duration
		m := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
			if len(paths) == 0 {
				keepAlives = append(keepAlives, now)
			}
		}, nil)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(args(urgent(press)))
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		start := time.Unix(1_800_000_000, 0)
		sub.TouchLastReport(start)
		n := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: switchCluster, EventID: initialPress})
		m.OnEventFired(subscription.EventFiring{Path: press, Number: n, Priority: im.EventPriorityInfo})
		for now = time.Second; now <= 50*time.Second; now += time.Second {
			m.Tick(context.Background(), start.Add(now))
		}
		if len(keepAlives) != 1 || keepAlives[0] != 48*time.Second {
			t.Fatalf("keep-alives at %v, want one at 48s", keepAlives)
		}
	})

	// What does not change.
	t.Run("an urgent event that is sent is the report of its interval", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, args(urgent(press)), true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		got := firstReports(b, 50*time.Second)
		if len(got) != 2 || got[0].hook != "event" || !equalNumbers(got[0].events, n) || got[0].at != time.Second ||
			!isKeepAlive(got[1]) || got[1].at != 49*time.Second {
			t.Fatalf("reports = %+v, want the event at 1s and the keep-alive a send interval after it, at 49s", got)
		}
	})

	t.Run("a non-urgent event that is sent is the send interval's report", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, args(im.ConcreteEventPath{}), true)
		n := b.emit(1, switchCluster, initialPress, im.EventPriorityInfo)
		got := firstReports(b, 96*time.Second)
		if len(got) != 2 || got[0].hook != "event" || !equalNumbers(got[0].events, n) || got[0].at != 48*time.Second ||
			!isKeepAlive(got[1]) || got[1].at != 96*time.Second {
			t.Fatalf("reports = %+v, want the event at 48s with no keep-alive beside it, and the keep-alive at 96s", got)
		}
	})

	t.Run("an attribute change is the report of its interval", func(t *testing.T) {
		t.Parallel()
		a := eventArgs(true, press)
		a.MinIntervalFloor = 1
		b := newEventReportBench(t, a, true)
		b.m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
		got := firstReports(b, 50*time.Second)
		if len(got) != 2 || got[0].hook != "attribute" || len(got[0].paths) != 1 || got[0].at != time.Second ||
			!isKeepAlive(got[1]) || got[1].at != 49*time.Second {
			t.Fatalf("reports = %+v, want the change at 1s and the keep-alive a send interval after it, at 49s", got)
		}
	})

	t.Run("an idle subscription's keep-alive leaves every send interval", func(t *testing.T) {
		t.Parallel()
		b := newEventReportBench(t, args(press), true)
		got := firstReports(b, 100*time.Second)
		if len(got) != 2 || !isKeepAlive(got[0]) || got[0].at != 48*time.Second || !isKeepAlive(got[1]) || got[1].at != 96*time.Second {
			t.Fatalf("reports = %+v, want keep-alives at 48s and 96s", got)
		}
	})
}
