// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/internal/inflight"
)

// run is the engine's tick loop. Iterates the live subscription set
// every Config.TickInterval, emitting:
//
//   - a report of the changed attributes, and of the events owed with
//     them, when MinIntervalFloor has elapsed — asked for by an attribute
//     change or by an event an urgent path of the subscriber covers,
//   - a report of what is owed, or an empty keep-alive, when the send
//     interval has elapsed.
//
// Closed subscriptions are skipped. The reporter callback is invoked
// outside the manager's lock so it can safely block on the wire.
//
// Shared engine ticker vs. matter.js per-subscription timer (by-design):
// matter.js (`packages/node/src/node/server/ServerSubscription.ts:191`) gives
// each ServerSubscription its own `Time.getTimer(sendInterval, callback)`.
// chip (`src/app/ReadHandler.cpp`) uses per-ReadHandler state inside the IM
// engine — one state machine per subscription. go-fabric uses a single shared
// ticker (default 250 ms) that iterates all subscriptions. The ticker granularity
// means a subscription's report can fire up to 250 ms early compared to a strict
// per-timer model. This is unobservable by any Matter commissioner (Apple Home,
// chip-tool) because the early-fire falls well within the spec-allowed window and
// the MinIntervalFloor gate already protects against over-reporting. The shared
// ticker is the idiomatic Go approach: per-subscription goroutines would create
// O(N_subscriptions) timers and goroutines for a problem that is trivially
// solved by a single shared poller. Documented in
// `notes/parity/by_design.md` §"Systematic Parity Run #02".
//
// The context a reporter receives ends when the engine does — with ctx or
// with [Manager.Stop] — so a report that waits on its peer can end its
// wait instead of holding Stop for the length of it (review II,
// W2-conc-06).
func (m *Manager) run(ctx context.Context) {
	defer m.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ticker := m.cfg.Clock.NewTicker(m.cfg.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case now := <-ticker.C():
			m.inflight.Enter(inflight.SubscriptionPending)
			m.tick(ctx, now)
			m.inflight.Leave(inflight.SubscriptionPending)
		}
	}
}

// tick walks every subscription once and starts, for each that has
// something due, its report on a goroutine of its own, returning the
// group of the reports it started. A subscription whose previous report
// is still on the wire is skipped — its changes stay queued for the next
// tick — so one peer that is slow to answer a chunk holds up only its own
// subscription, never the others: matter.js gives every
// ServerSubscription its own update promise
// (packages/node/src/node/server/ServerSubscription.ts #triggerSendUpdate
// / #currentUpdatePromise), and chip's ReportingEngine serves each
// ReadHandler on its own exchange. Before this, the reporter ran inline
// and a single chunk handshake waiting out its timeout stalled every
// other subscription's reports for that long (the CHIP harness lost its
// FabricSceneInfo reports to TC-S-2.6 that way).
func (m *Manager) tick(ctx context.Context, now time.Time) *sync.WaitGroup {
	m.mu.RLock()
	eventReporter := m.eventReporter
	m.mu.RUnlock()

	started := &sync.WaitGroup{}
	for _, sub := range m.snapshot() {
		if sub.IsClosed() || sub.isPriming() {
			continue
		}
		if !sub.reporting.CompareAndSwap(false, true) {
			continue
		}
		work := m.dueReport(ctx, sub, now, eventReporter)
		if work == nil {
			sub.reporting.Store(false)
			continue
		}
		m.wg.Add(1)
		started.Add(1)
		m.inflight.Enter(inflight.SubscriptionPending)
		go func() {
			defer m.wg.Done()
			defer started.Done()
			defer m.inflight.Leave(inflight.SubscriptionPending)
			defer sub.reporting.Store(false)
			work()
		}()
	}
	return started
}

// dueReport drains what sub has due at now and returns the report to send
// for it, or nil when nothing is due.
//
// One pass, one report: the attribute changes and the events that are due
// together leave in one ReportData, as matter.js ServerSubscription.ts
// :539-555 at db021da0 (#sendUpdate takes both and calls
// #sendUpdateMessage once). When to send is [Subscription.drainDue]'s.
//
// Which hook gets the report follows from what it carries:
//
//   - Events, with or without attribute paths, go to the [EventReporter];
//     the paths due with them wait on the subscription for it to take
//     ([Subscription.TakeDuePaths]). A reporter that does not take them
//     sends the events alone, and the paths are marked dirty again: they
//     follow in an attribute report of their own once MinIntervalFloor has
//     elapsed, never as a second report in the same tick.
//   - Attribute paths alone, and the keep-alive when the send interval
//     has elapsed with nothing owed, go to the [Reporter]. The send
//     interval is the matter.js-style cadence of
//     [Subscription.sendIntervalLocked], not MaxIntervalCeiling: Apple
//     Home's MTRDevice and chip-tool's ReadClient both drop a subscription
//     on a timer that fires well before the ceiling.
//
// Without an [EventReporter] the events drained are not sent.
func (m *Manager) dueReport(ctx context.Context, sub *Subscription, now time.Time, eventReporter EventReporter) func() {
	work, due := sub.drainDue(now, m.eventSourceOf(), false)
	if !due {
		return nil
	}
	if len(work.events) > 0 && eventReporter != nil {
		return func() {
			sub.offerDuePaths(work.paths)
			eventReporter(ctx, sub, work.events)
			if left := sub.TakeDuePaths(); len(left) > 0 {
				sub.RequeueAttributes(left)
			}
		}
	}
	if m.reporter == nil || (len(work.paths) == 0 && !work.keepAlive) {
		return nil
	}
	reporter := m.reporter
	return func() { reporter(ctx, sub, work.paths) }
}

// Tick is the test surface for [Manager.tick]: it waits for the reports
// the tick started, so a test observes them deterministically.
func (m *Manager) Tick(ctx context.Context, now time.Time) {
	m.tick(ctx, now).Wait()
}
