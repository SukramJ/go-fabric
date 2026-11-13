// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// maxSendInterval caps the publisher heartbeat so at least two reports
// fall inside the operational session manager's idle TTL
// (secure/operational.SessionIdleTimeout). See
// [Subscription.sendIntervalLocked] for why the cap exists;
// TestSendInterval_LongMaxIntervalStillBeatsSessionIdleTimeout drives
// the manager against that TTL so neither constant can drift out from
// under the other.
const maxSendInterval = 2 * time.Minute

// Subscription is one active commissioner-side subscription. The
// manager keeps these in a map keyed by ID; engine.Tick walks the
// map to drive reports.
type Subscription struct {
	// reporting is set while a report for this subscription is on the
	// wire: the engine starts no second one until it completes, as
	// matter.js ServerSubscription #triggerSendUpdate defers an update
	// while #currentUpdatePromise is pending.
	reporting atomic.Bool
	// ID is the bridge-allocated 32-bit subscription identifier
	// returned in SubscribeResponse (Matter §8.5.5).
	ID uint32
	// FabricIndex scopes the subscription to one fabric.
	FabricIndex uint8
	// PeerNodeID is the commissioner node that owns the subscription.
	PeerNodeID uint64
	// SessionID is the operational session id this subscription
	// belongs to. Manager.OnSessionClosed tears down every
	// subscription with a matching SessionID.
	SessionID uint16

	// MinIntervalFloor / MaxIntervalCeiling come from the request
	// (Matter §10.6.9). The negotiated cadence the bridge advertises
	// is min == MinIntervalFloor, max == MaxIntervalCeiling clamped
	// to manager limits.
	MinIntervalFloor   uint16
	MaxIntervalCeiling uint16
	// KeepSubscriptions mirrors the request flag; affects how the
	// commissioner re-establishes after disconnect.
	KeepSubscriptions bool
	// AttributePaths the subscription covers. Wildcards are honored
	// by the engine via expansion at evaluation time.
	AttributePaths []im.ConcreteAttributePath
	// EventPaths the subscription is interested in (Matter §10.6.9
	// EventRequests). Wildcards work via Has* flags. Empty slice = no
	// event fan-out for this subscription.
	EventPaths []im.ConcreteEventPath

	// sendIntervalOverride, when non-zero, replaces the derived heartbeat
	// cadence. Set only by [Manager.Restore], which re-creates a former
	// subscription with the send interval it negotiated in its first run
	// (matter.js ServerSubscription `useAsSendInterval`).
	sendIntervalOverride time.Duration

	// mu guards closed, deferredUntil, lastReport, pendingDirty, the
	// events* watermark and priming.
	mu sync.Mutex
	// priming is set while the subscription's priming report and
	// SubscribeResponse are still going out ([SubscribeArgs.Priming]);
	// the engine reports nothing for it until [Subscription.EndPriming].
	priming    bool
	lastReport time.Time
	// deferredUntil holds every report that carries data until then
	// (DelayReportData, [Subscription.DeferReports]); a keep-alive is not
	// held.
	deferredUntil time.Time
	pendingDirty  map[im.ConcreteAttributePath]struct{}
	// The events the subscription owes are one number, not a queue: the
	// lowest unreported event number (eventsMin, valid while
	// eventsPending) and whether one of them asks for a report
	// (eventsUrgent: an urgent path of the subscriber covers it, it was
	// owed when the priming report ended, or the report that carried it
	// could not be sent). The report reads
	// the occurrences from the event log at drain time, so an event storm
	// under a long MinIntervalFloor costs a subscription nothing and the
	// log's own bound bounds it. Mirrors matter.js
	// packages/node/src/node/server/ServerSubscription.ts:391-394
	// (#handleAddedEvents keeps #outstandingEventsMinNumber) and :541-542
	// (#sendUpdate takes it and reads the log).
	eventsPending bool
	eventsMin     uint64
	eventsUrgent  bool
	// eventsReported is the highest event number a drain has handed out
	// (valid while eventsHaveReported): a firing at or below it was read
	// from the log by an earlier drain already — the bridge appends an
	// event before it fans it out — and is not owed again.
	eventsReported     uint64
	eventsHaveReported bool
	// duePaths are the dirty attribute paths the engine drained together
	// with the events it is handing to the [EventReporter], until the
	// reporter takes them ([Subscription.TakeDuePaths]).
	duePaths []im.ConcreteAttributePath
	closed   bool
}

// TakeDuePaths returns the changed attribute paths that are due in the
// same report as the events the engine is handing to the [EventReporter],
// and hands them over: the reporter that takes them sends them with the
// events in one ReportData, and marks them dirty again
// ([Subscription.RequeueAttributes]) when that report cannot be sent. Nil
// outside an [EventReporter] call and when no attribute change is due.
//
// Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts
// :539-555 at db021da0: #sendUpdate takes #outstandingAttributeUpdates and
// #outstandingEventsMinNumber together and calls #sendUpdateMessage once.
// An [EventReporter] that does not call it sends its events alone; the
// engine then marks the paths dirty again, and they follow in an attribute
// report of their own once MinIntervalFloor has elapsed.
func (s *Subscription) TakeDuePaths() []im.ConcreteAttributePath {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := s.duePaths
	s.duePaths = nil
	return paths
}

// markDirty records that an attribute path has a pending change. The
// engine's tick finds it; nothing needs waking.
func (s *Subscription) markDirty(path im.ConcreteAttributePath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.pendingDirty == nil {
		s.pendingDirty = make(map[im.ConcreteAttributePath]struct{})
	}
	s.pendingDirty[path] = struct{}{}
}

// sendIntervalLocked computes the publisher-side heartbeat cadence.
// Caller must hold s.mu.
//
// Mirrors matter.js's `#determineSendingIntervals` formula in
// `packages/node/src/node/server/ServerSubscription.ts:259-300`
//
//	sendInterval = maxInterval / 2           // ideally half
//	if sendInterval < 60 s:
//	    sendInterval = max(MinIntervalFloor, maxInterval × 0.8)
//	if sendInterval < MinIntervalFloor:
//	    sendInterval = MinIntervalFloor
//
// The previous 5 s hard cap was an empirical workaround for a
// chip-tool `ExchangeContext` timeout observed at ~10.45 s — the
// audit found the workaround generates ~10× more wire traffic than
// matter.js for typical Apple subscription cadences (Apple
// negotiates ~max=60, so 48 s vs 5 s = ~10× delta) and likely
// triggers Apple's MTRDevice flood-protection heuristic. The proper
// fix for the chip exchange timeout is the per-chunk
// IM:StatusResponse wait applied separately; every chunk's StatusResponse
// resets the exchange's MRP timer.
//
// Randomisation window (by-design):
// matter.js adds `subscriptionRandomizationWindow * Math.random()` to
// the send interval
// (`packages/node/src/node/server/ServerSubscription.ts:282`) to
// distribute heartbeat traffic across a fleet of subscriptions that
// share the same JS event loop. chip (`src/app/ReadHandler.cpp:769`)
// does NOT apply any randomisation — it uses the negotiated
// MaxInterval directly. go-fabric follows the chip model: each
// subscription runs in an independent Go goroutine path and the
// shared-ticker architecture already provides natural phase scatter
// (subscriptions are created at different wall-clock offsets, so
// their lastReport stamps diverge organically). Per-subscription
// randomisation is therefore unnecessary and its omission is
// unobservable by any Matter commissioner. Documented in
// `notes/parity/by_design.md` §"Systematic Parity Run #02".
func (s *Subscription) sendIntervalLocked() time.Duration {
	if s.sendIntervalOverride > 0 {
		return s.sendIntervalOverride
	}
	maxInt := time.Duration(s.MaxIntervalCeiling) * time.Second
	if maxInt <= 0 {
		// Defensive: degenerate subscription — 30 s heartbeat so the
		// peer's keep-alive timer does not fire.
		return 30 * time.Second
	}

	send := maxInt / 2
	if send < time.Minute {
		// matter.js: if half-interval is under 1 minute (most Apple
		// Home subscriptions negotiate ~60 s max), use 0.8 ×
		// maxInterval so two retransmit attempts fit inside one
		// publisher cycle.
		eighty := time.Duration(float64(maxInt) * 0.8)
		floor := time.Duration(s.MinIntervalFloor) * time.Second
		send = max(floor, eighty)
	}
	// Upper clamp — a deliberate divergence from matter.js, which has no
	// cap because it never evicts idle operational sessions. This daemon
	// runs a 5-minute idle reaper on the operational session manager, and
	// on an otherwise quiet CASE session the heartbeat report (plus the
	// controller's ack) is the only thing that refreshes session
	// activity. A subscription that negotiated a long MaxIntervalCeiling
	// — the bridge accepts up to an hour — would otherwise have its
	// session reaped mid-cycle and be torn down every five minutes.
	// Keeping two heartbeats inside the TTL also survives one lost
	// report.
	if send > maxSendInterval {
		send = maxSendInterval
	}
	// Final clamp: never below the requested MinIntervalFloor
	// (commissioner-side rate limit) and never below 1 s.
	floor := time.Duration(s.MinIntervalFloor) * time.Second
	if send < floor {
		send = floor
	}
	if send < time.Second {
		send = time.Second
	}
	return send
}

// SendInterval returns the publisher heartbeat cadence the engine uses for
// this subscription — matter.js ServerSubscription.sendInterval.
func (s *Subscription) SendInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendIntervalLocked()
}

// Close marks the subscription closed; the engine skips it on the
// next tick and the manager removes it.
func (s *Subscription) Close() {
	s.mu.Lock()
	s.closed = true
	s.pendingDirty = nil
	s.mu.Unlock()
}

// IsClosed reports whether [Close] was called.
func (s *Subscription) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// touchLastReport stamps the report time without consuming what the
// subscription owes.
func (s *Subscription) touchLastReport(now time.Time) {
	s.mu.Lock()
	s.lastReport = now
	s.mu.Unlock()
}

// EndPriming marks the priming report and the SubscribeResponse as sent:
// the subscription is established, and its report cadence starts now.
// Changes marked dirty while it was priming stay queued and go out once
// MinInterval has elapsed, as matter.js reports what changed during the
// initial report afterwards (ServerSubscription.ts: the subscription is
// activated only once the initial report completed — activate() sets
// #sendUpdatesActivated, packages/node/src/node/server/ServerSubscription.ts:412).
// The events fired in that window go with them, urgent or not: matter.js
// sends whatever is outstanding at activation, attribute changes and
// events alike (ServerSubscription.ts:443-445 at db021da0) — at once
// there, after MinIntervalFloor here.
func (s *Subscription) EndPriming(now time.Time) {
	s.mu.Lock()
	s.priming = false
	s.lastReport = now
	if s.eventsPending {
		s.eventsUrgent = true
	}
	s.mu.Unlock()
}

// isPriming reports whether the subscription is still being established.
func (s *Subscription) isPriming() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.priming
}

// TouchLastReport is the exported counterpart of [touchLastReport].
// The bridge calls it right after the initial-report flush so the
// engine doesn't fire an immediate keep-alive on the next 250 ms tick
// (a subscription that never reported owes its keep-alive at once — see
// [Subscription.drainDue]).
func (s *Subscription) TouchLastReport(now time.Time) {
	s.touchLastReport(now)
}

// dueWork is what one report of a subscription carries.
type dueWork struct {
	// events are the owed events the subscription selects, in number
	// order; paths the changed attribute paths.
	events []im.EventReport
	paths  []im.ConcreteAttributePath
	// keepAlive: the send interval has elapsed since the last report, so
	// a report goes out even when it carries nothing.
	keepAlive bool
}

// drainDue decides whether a report of the subscription is due at now and
// takes what that report carries: the changed attribute paths and the
// owed events together, for one report. due is false when nothing is
// sent, and then nothing was taken but an event watermark that yielded no
// event.
//
// Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts at
// db021da0:
//
//   - A report is asked for by an attribute change (:380-382) or by an
//     event an urgent path of the subscriber covers (:396-398); an event
//     no urgent path covers is owed and asks for nothing.
//   - What was asked for waits for max(lastUpdateTime + minIntervalFloor,
//     deferredUntil) (#prepareDataUpdate, :464-473) — an urgent event as
//     an attribute change, whatever the event's priority.
//   - The send interval's pass (#startSendInterval, :480-485) sends
//     without being asked: what is owed, or a keep-alive when nothing is.
//   - One pass takes the attribute changes and the events' minimum number
//     together and stamps the time of the last report once (#sendUpdate,
//     :539-551).
//
// flush takes everything whatever the intervals, for the report of a
// fabric that leaves (#flush, :753-763); it sends no keep-alive.
//
// A pass that was asked for by an urgent event alone and finds no event
// to send — no source, the event gone from the log, none of the
// subscription's paths — is not a report: the time of the last report
// stays, so a dirty attribute past its floor and the keep-alive are not
// held back by a report that was never sent.
func (s *Subscription) drainDue(now time.Time, src EventSource, flush bool) (work dueWork, due bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return dueWork{}, false
	}
	takeData := flush
	if !flush {
		work.keepAlive = s.lastReport.IsZero() || now.Sub(s.lastReport) >= s.sendIntervalLocked()
		asked := len(s.pendingDirty) > 0 || (s.eventsPending && s.eventsUrgent)
		floorOver := s.lastReport.IsZero() || now.Sub(s.lastReport) >= time.Duration(s.MinIntervalFloor)*time.Second
		held := now.Before(s.deferredUntil)
		takeData = !held && (work.keepAlive || (asked && floorOver))
	}
	if takeData {
		work.events = s.takeEventsLocked(src)
		work.paths = s.takeDirtyLocked()
	}
	if len(work.events) == 0 && len(work.paths) == 0 && !work.keepAlive {
		return dueWork{}, false
	}
	s.lastReport = now
	return work, true
}

// takeDirtyLocked hands out the dirty attribute paths. Caller must hold
// s.mu.
func (s *Subscription) takeDirtyLocked() []im.ConcreteAttributePath {
	if len(s.pendingDirty) == 0 {
		return nil
	}
	out := make([]im.ConcreteAttributePath, 0, len(s.pendingDirty))
	for p := range s.pendingDirty {
		out = append(out, p)
	}
	s.pendingDirty = nil
	return out
}

// offerDuePaths holds paths for the [EventReporter] the engine is about
// to call ([Subscription.TakeDuePaths]).
func (s *Subscription) offerDuePaths(paths []im.ConcreteAttributePath) {
	s.mu.Lock()
	s.duePaths = paths
	s.mu.Unlock()
}

// queueEvent records that the event numbered number is owed to the
// subscriber: it lowers the watermark to number and, when urgent — an
// urgent path of the subscriber covers the event — asks for a report.
// Mirrors matter.js ServerSubscription.ts:385-399 at db021da0
// (#handleAddedEvents: the number is kept for every event, :392-394, and
// #prepareDataUpdate runs only for an urgent one, :396-398); the minimum
// rather than the first number keeps a firing that lost a race to a later
// one in the report.
func (s *Subscription) queueEvent(number uint64, urgent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.eventsHaveReported && number <= s.eventsReported {
		return
	}
	if !s.eventsPending || number < s.eventsMin {
		s.eventsMin = number
	}
	s.eventsPending = true
	if urgent {
		s.eventsUrgent = true
	}
}

// eventUrgency reports whether one of the subscription's event paths
// covers path, and whether one that does is urgent. Mirrors matter.js
// ServerSubscription.ts:401-409 at db021da0 (#sendEventUrgently: some
// request path with isUrgent whose endpoint, cluster and event are each
// absent or equal to the event's); the event's priority is not read.
func (s *Subscription) eventUrgency(path im.ConcreteEventPath) (selected, urgent bool) {
	for _, p := range s.EventPaths {
		if !p.Matches(path) {
			continue
		}
		selected = true
		if p.IsUrgent {
			return true, true
		}
	}
	return selected, false
}

// RequeueEvents owes the subscriber every selected event from number
// fromNumber on again: the reporter calls it when a report carrying
// them could not be sent. Mirrors matter.js ServerSubscription.ts:582-584
// at db021da0 ("newer number are always higher, so we can just set it").
// The events ask for a report again, urgent or not, so the retry goes out
// once MinIntervalFloor has elapsed since the failed report — as the
// paths of [Subscription.RequeueAttributes] do, and as both did before
// events were reported by urgency. matter.js schedules nothing there
// (:569-584 never calls #prepareDataUpdate): its retry waits for the next
// change, urgent event or send interval.
func (s *Subscription) RequeueEvents(fromNumber uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if !s.eventsPending || fromNumber < s.eventsMin {
		s.eventsMin = fromNumber
	}
	s.eventsPending = true
	s.eventsUrgent = true
	if s.eventsHaveReported && s.eventsReported >= fromNumber {
		if fromNumber == 0 {
			s.eventsHaveReported = false
		} else {
			s.eventsReported = fromNumber - 1
		}
	}
}

// RequeueAttributes marks paths dirty again: the reporter calls it when
// a report carrying them could not be sent, so the next report retries
// them. Mirrors matter.js ServerSubscription.ts:567-577, which puts the
// attribute filter back with #addOutstandingAttributes.
func (s *Subscription) RequeueAttributes(paths []im.ConcreteAttributePath) {
	for _, p := range paths {
		s.markDirty(p)
	}
}

// takeEventsLocked hands out the events the subscription owes: it takes
// the watermark and reads the subscription's events from src. Number
// order and timestamps are the log's, the same as a Read answers
// (matter.js ServerSubscription.ts:874-887 at db021da0 reads the
// occurrences from the events' minimum number on). Caller must hold s.mu.
func (s *Subscription) takeEventsLocked(src EventSource) []im.EventReport {
	if !s.eventsPending {
		return nil
	}
	minNumber := s.eventsMin
	s.eventsPending = false
	s.eventsUrgent = false
	if src == nil {
		return nil
	}
	records := src.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, minNumber)
	if len(records) == 0 {
		return nil
	}
	s.eventsReported = records[len(records)-1].Number
	s.eventsHaveReported = true
	out := make([]im.EventReport, 0, len(records))
	for _, rec := range records {
		path := im.ConcreteEventPath{
			HasEndpoint: true, HasCluster: true, HasEvent: true,
			Endpoint: rec.Endpoint, Cluster: rec.Cluster, Event: rec.EventID,
		}
		if !s.selectsEvent(path) {
			continue
		}
		out = append(out, im.EventReport{
			Path:      path,
			Number:    rec.Number,
			Priority:  rec.Priority,
			Timestamp: rec.EpochMS,
			Data:      im.AttributeValue{Value: rec.Payload},
		})
	}
	return out
}

// selectsEvent reports whether one of the subscription's event paths
// covers path.
func (s *Subscription) selectsEvent(path im.ConcreteEventPath) bool {
	for _, p := range s.EventPaths {
		if p.Matches(path) {
			return true
		}
	}
	return false
}

// deferSendSlack is how far before its send interval a deferral ends at
// the latest: matter.js ServerSubscription SEND_DELAY.
const deferSendSlack = 50 * time.Millisecond

// DeferReports holds the subscription's next report off by delay, once:
// matter.js ServerSubscription.deferReports, which an InvokeRequest's
// DelayReportData triggers. A deferral that is still running is only ever
// shortened, and none holds a report past the send interval after the
// last report (the keep-alive due then).
func (s *Subscription) DeferReports(now time.Time, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	until := now.Add(delay)
	if !s.lastReport.IsZero() {
		if due := s.lastReport.Add(s.sendIntervalLocked() - deferSendSlack); due.Before(until) {
			until = due
		}
	}
	if s.deferredUntil.After(now) && s.deferredUntil.Before(until) {
		until = s.deferredUntil
	}
	s.deferredUntil = until
}

// selectsAnyEndpoint reports whether a path of the subscription selects
// one of endpoints, or every endpoint (matter.js
// ServerSubscription.selectsAnyEndpoint).
func (s *Subscription) selectsAnyEndpoint(endpoints map[uint16]bool) bool {
	for _, p := range s.AttributePaths {
		if !p.HasEndpoint || endpoints[p.Endpoint] {
			return true
		}
	}
	for _, p := range s.EventPaths {
		if !p.HasEndpoint || endpoints[p.Endpoint] {
			return true
		}
	}
	return false
}
