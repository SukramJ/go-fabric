// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

// Ports by meaning of matter.js
// packages/node/test/node/ServerSubscriptionTest.ts at b25ed960, the
// subscription-side rows of the session supersession cases (:1490-1882):
// the move onto a superseding session ([Manager.MoveSession]) and the
// resend at once of a report that failed on the session the subscription
// left. The failure count a move resets is the bridge's
// (bridge/supersession_parity_matterjs_test.go); the supersession itself
// is secure/operational's.

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
)

var (
	supersessionT0   = time.Unix(1_700_000_000, 0)
	supersessionPath = im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x28, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
)

// sentReport is one report a test sender saw: the session the
// subscription reported on when it was handed over, and what it carried.
type sentReport struct {
	session uint16
	report  Report
	at      time.Time
}

// recordingSender records every report and lets fail decide, per report,
// how its sending ends: it may move the subscription and owe the data
// again, as the bridge's sender does for a failed report.
type recordingSender struct {
	mu    sync.Mutex
	clk   *clocktest.Fake
	sent  []sentReport
	onRep func(sub *Subscription, r Report, index int)
}

func (s *recordingSender) send(_ context.Context, sub *Subscription, r Report) ReportOutcome {
	s.mu.Lock()
	index := len(s.sent)
	s.sent = append(s.sent, sentReport{session: sub.CurrentSessionID(), report: r, at: s.clk.Now()})
	on := s.onRep
	s.mu.Unlock()
	if on != nil {
		on(sub, r, index)
	}
	return Sent
}

func (s *recordingSender) reports() []sentReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sent)
}

func newSupersessionManager(t *testing.T) (*Manager, *recordingSender, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(supersessionT0)
	m := NewManager(Config{Clock: clk}, nil, nil)
	rs := &recordingSender{clk: clk}
	m.SetReportSender(rs.send)
	return m, rs, clk
}

func subscribeOn(t *testing.T, m *Manager, sessionID uint16, priming bool) *Subscription {
	t.Helper()
	sub, err := m.Subscribe(SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0x1234, SessionID: sessionID, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{}}, Priming: priming,
	})
	if err != nil {
		t.Fatal(err)
	}
	sub.TouchLastReport(supersessionT0)
	return sub
}

// owedAgainUncounted is what the bridge's sender does with a report that
// failed on a session the subscription has left: the data is owed again,
// no failed update counted.
func owedAgainUncounted(sub *Subscription, r Report) {
	if len(r.Events) > 0 {
		sub.RequeueAbortedEvents(r.Events[0].Number)
	}
	sub.RequeueAbortedAttributes(r.Paths)
}

func TestServerSubscriptionSupersession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("moves to the newer session and reports there after the former session closes", func(t *testing.T) {
		t.Parallel()
		m, rs, _ := newSupersessionManager(t)
		var changed []uint16
		m.SetOnSessionSubscriptionsChanged(func(id uint16) { changed = append(changed, id) })
		sub := subscribeOn(t, m, 1, false)
		changed = nil

		if moved := m.MoveSession(1, 2); !slices.Equal(moved, []uint32{sub.ID}) {
			t.Fatalf("MoveSession moved %v; want [%d]", moved, sub.ID)
		}
		if got := sub.CurrentSessionID(); got != 2 {
			t.Fatalf("CurrentSessionID = %d; want 2", got)
		}
		if want := []uint16{1, 2}; !slices.Equal(changed, want) {
			t.Fatalf("change hook saw %v; want %v (the former and the newer session)", changed, want)
		}
		if counts := m.CountBySession(); counts[1] != 0 || counts[2] != 1 {
			t.Fatalf("CountBySession = %v; want the subscription on session 2 only", counts)
		}

		m.CloseSession(1)
		if sub.IsClosed() {
			t.Fatal("the former session's close closed the moved subscription")
		}
		m.OnAttributeChanged(supersessionPath)
		m.Tick(ctx, supersessionT0.Add(time.Second))
		m.Tick(ctx, supersessionT0.Add(time.Second+sub.SendInterval()))
		got := rs.reports()
		if len(got) != 2 || got[0].session != 2 || got[1].session != 2 {
			t.Fatalf("reports = %+v; want two, both on the newer session", got)
		}
	})

	t.Run("follows a newer session set up while its initial report is sent", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newSupersessionManager(t)
		sub := subscribeOn(t, m, 1, true)
		m.MoveSession(1, 2)
		if got := sub.CurrentSessionID(); got != 2 {
			t.Fatalf("CurrentSessionID of the priming subscription = %d; want 2", got)
		}
		m.CloseSession(1)
		if sub.IsClosed() {
			t.Fatal("the former session's close closed the priming subscription that moved")
		}
		sub.EndPriming(supersessionT0)
		if counts := m.CountBySession(); counts[2] != 1 {
			t.Fatalf("CountBySession = %v; want the established subscription on session 2", counts)
		}
	})

	t.Run("stays on a session that was superseded before the subscription was set up", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newSupersessionManager(t)
		m.MoveSession(1, 2)
		sub := subscribeOn(t, m, 1, false)
		if got := sub.CurrentSessionID(); got != 1 {
			t.Fatalf("CurrentSessionID = %d; want 1", got)
		}
	})

	t.Run("ignores a newer session once closed", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newSupersessionManager(t)
		var changed []uint16
		m.SetOnSessionSubscriptionsChanged(func(id uint16) { changed = append(changed, id) })
		sub := subscribeOn(t, m, 1, false)
		if err := m.Close(sub.ID); err != nil {
			t.Fatal(err)
		}
		changed = nil
		if moved := m.MoveSession(1, 2); len(moved) != 0 {
			t.Fatalf("MoveSession moved %v; want nothing", moved)
		}
		if sub.CurrentSessionID() != 1 || len(changed) != 0 {
			t.Fatalf("closed subscription on %d, hook %v; want it left on 1, no hook", sub.CurrentSessionID(), changed)
		}
	})

	t.Run("resends at once on its new session when a send on the session it left fails", func(t *testing.T) {
		t.Parallel()
		m, rs, clk := newSupersessionManager(t)
		sub := subscribeOn(t, m, 1, false)
		// The subscriber sets up a new session while each of the first
		// three sends is under way, and each of them fails.
		rs.onRep = func(sub *Subscription, r Report, index int) {
			if index >= 3 {
				return
			}
			m.MoveSession(uint16(index+1), uint16(index+2))
			clk.Advance(10 * time.Millisecond)
			owedAgainUncounted(sub, r)
		}
		m.OnAttributeChanged(supersessionPath)
		m.Tick(ctx, supersessionT0.Add(200*time.Millisecond))

		got := rs.reports()
		if len(got) != 4 {
			t.Fatalf("%d reports in one pass; want 4 (the failed one and three resends at once)", len(got))
		}
		for i, r := range got {
			if r.session != uint16(i+1) {
				t.Fatalf("report %d on session %d; want %d", i, r.session, i+1)
			}
			if !slices.Equal(r.report.Paths, []im.ConcreteAttributePath{supersessionPath}) {
				t.Fatalf("report %d carried %v; want the changed path", i, r.report.Paths)
			}
			if i > 0 && !r.at.Equal(got[i-1].at.Add(10*time.Millisecond)) {
				t.Fatalf("report %d sent at %v; want at the failure of the one before (%v)", i, r.at, got[i-1].at.Add(10*time.Millisecond))
			}
		}
		if sub.CurrentSessionID() != 4 || sub.IsClosed() {
			t.Fatalf("subscription on %d, closed %v; want on session 4, open", sub.CurrentSessionID(), sub.IsClosed())
		}
		if n := sub.failedUpdateCount(); n != 0 {
			t.Fatalf("%d failed updates counted; want none", n)
		}
	})

	t.Run("resends a keep-alive at once on its new session when it failed on the session it left", func(t *testing.T) {
		t.Parallel()
		// Not a matter.js row: the keep-alive half of :569-577
		// (#keepAliveOwed ||= !onlyWithData) — a keep-alive that failed on
		// the session the subscription left goes out again, empty.
		m, rs, _ := newSupersessionManager(t)
		sub := subscribeOn(t, m, 1, false)
		rs.onRep = func(sub *Subscription, r Report, index int) {
			if index == 0 {
				m.MoveSession(1, 2)
				owedAgainUncounted(sub, r)
			}
		}
		m.Tick(ctx, supersessionT0.Add(sub.SendInterval()))
		got := rs.reports()
		if len(got) != 2 || !got[0].report.KeepAlive || !got[1].report.KeepAlive || got[1].session != 2 {
			t.Fatalf("reports = %+v; want the keep-alive and its resend on session 2", got)
		}
	})

	t.Run("does not resend at once when a report aborted on its own session", func(t *testing.T) {
		t.Parallel()
		// Not a matter.js row: an abort that is not the left session's —
		// the engine's stop — owes the data again for the next report only.
		m, rs, _ := newSupersessionManager(t)
		subscribeOn(t, m, 1, false)
		rs.onRep = func(sub *Subscription, r Report, index int) {
			if index == 0 {
				owedAgainUncounted(sub, r)
			}
		}
		m.OnAttributeChanged(supersessionPath)
		m.Tick(ctx, supersessionT0.Add(time.Second))
		if got := rs.reports(); len(got) != 1 {
			t.Fatalf("%d reports; want 1", len(got))
		}
	})

	t.Run("resends attribute and event data of an update that failed on the session it left (characterization)", func(t *testing.T) {
		t.Parallel()
		m, rs, _ := newSupersessionManager(t)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0x1234, SessionID: 1, MaxIntervalCeiling: 60,
			AttributePaths: []im.ConcreteAttributePath{{}},
			EventPaths:     []im.ConcreteEventPath{{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		sub.TouchLastReport(supersessionT0)
		rs.onRep = func(sub *Subscription, r Report, index int) {
			if index == 0 {
				m.MoveSession(1, 2)
				owedAgainUncounted(sub, r)
			}
		}
		number := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 0, Cluster: 0x28, EventID: 0, Payload: uint8(1)})
		m.OnEventFired(EventFiring{Path: im.ConcreteEventPath{Cluster: 0x28, HasEndpoint: true, HasCluster: true, HasEvent: true}, Number: number, Priority: im.EventPriorityInfo})
		m.OnAttributeChanged(supersessionPath)
		m.Tick(ctx, supersessionT0.Add(time.Second))

		got := rs.reports()
		if len(got) != 2 {
			t.Fatalf("%d reports; want the failed one and its resend", len(got))
		}
		failed, resent := got[0], got[1]
		if failed.session != 1 || resent.session != 2 {
			t.Fatalf("sessions %d, %d; want 1 then 2", failed.session, resent.session)
		}
		if len(failed.report.Events) != 1 || len(failed.report.Paths) != 1 {
			t.Fatalf("failed report = %+v; want one path and one event", failed.report)
		}
		if !slices.Equal(resent.report.Paths, failed.report.Paths) || len(resent.report.Events) != 1 || resent.report.Events[0].Number != failed.report.Events[0].Number {
			t.Fatalf("resent report = %+v; want the failed report's data", resent.report)
		}
	})

	t.Run("resends attribute and event data of an update that failed on its own session (characterization)", func(t *testing.T) {
		t.Parallel()
		m, rs, _ := newSupersessionManager(t)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0x1234, SessionID: 1, MaxIntervalCeiling: 60,
			AttributePaths: []im.ConcreteAttributePath{{}},
			EventPaths:     []im.ConcreteEventPath{{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		sub.TouchLastReport(supersessionT0)
		rs.onRep = func(sub *Subscription, r Report, index int) {
			if index == 0 {
				sub.RequeueEvents(r.Events[0].Number)
				sub.RequeueAttributes(r.Paths)
			}
		}
		number := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 0, Cluster: 0x28, EventID: 0, Payload: uint8(1)})
		m.OnEventFired(EventFiring{Path: im.ConcreteEventPath{Cluster: 0x28, HasEndpoint: true, HasCluster: true, HasEvent: true}, Number: number, Priority: im.EventPriorityInfo})
		m.OnAttributeChanged(supersessionPath)
		m.Tick(ctx, supersessionT0.Add(time.Second))
		if got := rs.reports(); len(got) != 1 {
			t.Fatalf("%d reports in the failing pass; want 1 (no resend at once on its own session)", len(got))
		}
		m.Tick(ctx, supersessionT0.Add(sub.SendInterval()+time.Second))
		got := rs.reports()
		if len(got) != 2 {
			t.Fatalf("%d reports; want the failed one and the next", len(got))
		}
		failed, resent := got[0], got[1]
		if resent.session != 1 || !slices.Equal(resent.report.Paths, failed.report.Paths) || len(resent.report.Events) != 1 || resent.report.Events[0].Number != failed.report.Events[0].Number {
			t.Fatalf("next report = %+v; want the failed report's data on session 1", resent)
		}
	})
}
