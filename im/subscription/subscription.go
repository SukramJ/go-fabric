// SPDX-License-Identifier: MIT
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

	mu sync.Mutex
	// priming is set while the subscription's priming report and
	// SubscribeResponse are still going out ([SubscribeArgs.Priming]);
	// the engine reports nothing for it until [Subscription.EndPriming].
	priming    bool
	lastReport time.Time
	// deferredUntil holds every report but a critical event until then
	// (DelayReportData, [Subscription.DeferReports]).
	deferredUntil time.Time
	pendingDirty  map[im.ConcreteAttributePath]struct{}
	pendingEvents []pendingEvent
	closed        bool
}

// pendingEvent captures one event firing that has not yet been
// flushed to the commissioner. The engine drains these on the same
// MinInterval cadence as attribute reports.
type pendingEvent struct {
	Path      im.ConcreteEventPath
	Number    uint64
	Priority  im.EventPriority
	Timestamp uint64
	Data      im.AttributeValue
}

// markDirty records that an attribute path has a pending change.
// Returns true on first transition from clean to dirty.
func (s *Subscription) markDirty(path im.ConcreteAttributePath) (firstDirty bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.pendingDirty == nil {
		s.pendingDirty = make(map[im.ConcreteAttributePath]struct{})
	}
	if _, in := s.pendingDirty[path]; in {
		return false
	}
	s.pendingDirty[path] = struct{}{}
	return len(s.pendingDirty) == 1
}

// heartbeatIntervalElapsed reports whether the publisher-initiated
// heartbeat deadline has fired. Without a periodic keep-alive both
// commissioners drop the subscription with `CHIP Error 0x32 Timeout`
// / `MTRErrorDomain Code=9 Transaction timed out` — the chip-tool-
// reproducible Subscribe-pump bug from Session 14 §19.4.
func (s *Subscription) heartbeatIntervalElapsed(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReport.IsZero() {
		return true
	}
	return now.Sub(s.lastReport) >= s.sendIntervalLocked()
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

// touchLastReport stamps the report time without consuming dirty
// paths — used for keep-alives that emit zero-attribute reports.
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
func (s *Subscription) EndPriming(now time.Time) {
	s.mu.Lock()
	s.priming = false
	s.lastReport = now
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
// (lastReport.IsZero() makes maxIntervalElapsed return true on the
// first sweep — see [Subscription.maxIntervalElapsed]).
func (s *Subscription) TouchLastReport(now time.Time) {
	s.touchLastReport(now)
}

// drainDirtyIfElapsed returns the dirty paths *only* when
// MinIntervalFloor has elapsed since the last report; otherwise
// returns nil and leaves the dirty bucket alone.
func (s *Subscription) drainDirtyIfElapsed(now time.Time) []im.ConcreteAttributePath {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.pendingDirty) == 0 {
		return nil
	}
	if !s.lastReport.IsZero() && now.Sub(s.lastReport) < time.Duration(s.MinIntervalFloor)*time.Second {
		return nil
	}
	if now.Before(s.deferredUntil) {
		return nil
	}
	out := make([]im.ConcreteAttributePath, 0, len(s.pendingDirty))
	for p := range s.pendingDirty {
		out = append(out, p)
	}
	s.pendingDirty = nil
	s.lastReport = now
	return out
}

// queueEvent records an event firing that should ride the next
// report. Returns true on the first queued event for this
// subscription (so callers can wake the engine).
func (s *Subscription) queueEvent(ev pendingEvent) (firstQueued bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	first := len(s.pendingEvents) == 0
	s.pendingEvents = append(s.pendingEvents, ev)
	return first
}

// drainEventsIfElapsed mirrors drainDirtyIfElapsed for events.
// Critical-priority events bypass the MinIntervalFloor gate per
// Matter §10.6.6 — controllers expect immediate delivery.
func (s *Subscription) drainEventsIfElapsed(now time.Time) []pendingEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.pendingEvents) == 0 {
		return nil
	}
	hasCritical := false
	for _, ev := range s.pendingEvents {
		if ev.Priority == im.EventPriorityCritical {
			hasCritical = true
			break
		}
	}
	if !hasCritical && !s.lastReport.IsZero() && now.Sub(s.lastReport) < time.Duration(s.MinIntervalFloor)*time.Second {
		return nil
	}
	if !hasCritical && now.Before(s.deferredUntil) {
		return nil
	}
	out := s.pendingEvents
	s.pendingEvents = nil
	s.lastReport = now
	return out
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
