// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/schema"
)

// Errors.
var (
	// ErrFabricQuotaExceeded is returned when [Manager.Subscribe]
	// would exceed [Config.MaxSubscriptionsPerFabric] for the
	// requested fabric.
	ErrFabricQuotaExceeded = errors.New("subscription: per-fabric quota exceeded")
	// ErrCadenceOutOfRange is returned for MinInterval > MaxInterval
	// or values outside Matter §10.6.9 caps.
	ErrCadenceOutOfRange = errors.New("subscription: cadence out of range")
	// ErrCadenceInvertedAfterClamp is returned when the post-clamp
	// cadence is inverted: MinIntervalFloor (floored up to the
	// configured minimum) exceeds MaxIntervalCeiling (capped down to
	// the configured ceiling). Example: cfg.MaxIntervalCeilingSeconds=5,
	// request min=10, max=30 → after clamp min=10 > max=5. A silent
	// inversion causes a change report to never fire because the
	// floor can never elapse relative to a ceiling that is below it.
	ErrCadenceInvertedAfterClamp = errors.New("subscription: cadence inverted after clamp")
	// ErrNotFound is returned when a Get / Close hits no subscription.
	ErrNotFound = errors.New("subscription: not found")
	// ErrIDInUse is returned by [Manager.Restore] when the former
	// subscription's id already names a live subscription.
	ErrIDInUse = errors.New("subscription: id already in use")
)

// Reporter receives ReportData payloads the engine produces. The
// caller wires this into the bridge's IM dispatcher to encode +
// transmit the report on the right secure-channel session. ctx ends
// when the engine stops ([Manager.Stop]); a reporter that waits on its
// peer ends the wait with it.
type Reporter func(ctx context.Context, sub *Subscription, paths []im.ConcreteAttributePath)

// EventReporter is the hook for a report that carries events. The engine
// invokes it when a report of the subscription is due and events are owed
// — see [Subscription.TakeDuePaths] for the attribute changes due in the
// same report, which a reporter that sends one ReportData for both takes.
// An event does not leave by its priority: one that an urgent path of the
// subscriber covers asks for a report, which waits for MinIntervalFloor
// and a DelayReportData deferral as an attribute change does; any other
// leaves with the subscription's next report. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:385-409 and
// :454-478 at db021da0. A reporter that finds nothing left to send for the
// subscriber — every event filtered by its access check — calls
// [Subscription.NothingSent] instead of sending an empty report; the
// engine then sends the keep-alive if one is due and does not count the
// pass otherwise. Optional — a nil hook means events fan into the
// queue but never leave the bridge (useful for unit tests of the cadence
// math alone).
type EventReporter func(ctx context.Context, sub *Subscription, events []im.EventReport)

// FlushReporter receives everything a subscription owes when its fabric
// leaves ([Manager.FlushFabric]): the drained events and the dirty
// attribute paths together, for one report. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:534-556 at db021da0
// (#sendUpdate takes the attribute filter and the events' minimum number
// together) and :926-936 (#sendUpdateMessage sends both in one
// DataReport). [Reporter] and [EventReporter] stay the ongoing paths; an
// ongoing report with events carries its attribute changes too
// ([Subscription.TakeDuePaths]).
type FlushReporter func(ctx context.Context, sub *Subscription, events []im.EventReport, paths []im.ConcreteAttributePath)

// EventSource is where the engine reads a subscription's events when its
// report is due: the node's event log, which [im.EventLog] is. A
// subscription keeps only the lowest event number it owes, never the
// events (matter.js packages/node/src/node/server/ServerSubscription.ts
// :391-394, :541-542), so without a source no event report is sent.
type EventSource interface {
	Query(endpoint uint16, cluster, eventID uint32, minNumber uint64) []im.EventRecord
}

// The defaults a zero [Config] field takes.
const (
	// DefaultMaxSubscriptionsPerFabric is the per-fabric subscription
	// cap. The module's own: matter.js keeps no per-fabric quota (see
	// notes/parity/by_design.md, "Subscribe per-fabric quota eviction");
	// 16 is above the Matter floor of 3 (§8.5.4) with room for a home's
	// controllers.
	DefaultMaxSubscriptionsPerFabric = 16
	// DefaultMaxIntervalCeilingSeconds is the publisher's ceiling: one
	// hour. matter.js: MAX_INTERVAL_PUBLISHER_LIMIT = Hours.one
	// (packages/node/src/node/server/ServerSubscription.ts:71 at
	// 862ad75f); chip: kSubscriptionMaxIntervalPublisherLimit.
	DefaultMaxIntervalCeilingSeconds = 3600
	// DefaultTickInterval is the report sweeper's period.
	DefaultTickInterval = 250 * time.Millisecond
)

// Config tunes the manager.
type Config struct {
	// MaxSubscriptionsPerFabric caps the number of active
	// subscriptions per fabric. Matter floor is 3 (§8.5.4); 0 is
	// [DefaultMaxSubscriptionsPerFabric].
	MaxSubscriptionsPerFabric int
	// MinIntervalFloorSeconds lifts the MinIntervalFloor a subscriber
	// requests to at least this many seconds. Zero (the default) honours
	// the request as it is, as matter.js does: ServerSubscription holds a
	// report only until lastUpdateTime + the requested minIntervalFloor
	// (packages/node/src/node/server/ServerSubscription.ts
	// #prepareDataUpdate). A floor the subscriber did not ask for delays
	// every report that follows another within it — TC-CC-2.2 read a
	// RemainingTime reported 1.1 s after its command, 12 tenths short.
	// The shared tick (TickInterval) still batches what changes within it.
	MinIntervalFloorSeconds uint16
	// MaxIntervalCeilingSeconds is the upper bound the bridge will
	// accept. The bridge negotiates *down* to this value if the
	// commissioner asked for more. 0 is [DefaultMaxIntervalCeilingSeconds]
	// (1 h) — Apple Home and Google Home both stay well below.
	MaxIntervalCeilingSeconds uint16
	// TickInterval drives the engine's report sweeper. Smaller =
	// finer-grained MinIntervalFloor enforcement; larger = lower
	// goroutine overhead. Default 250 ms.
	TickInterval time.Duration
	// Clock is the engine's time source: the tick and every report
	// timestamp. nil is [clock.System].
	Clock clock.Clock
}

// Manager owns the active subscription set and runs the report
// engine. NewManager + Start + Stop is the lifecycle.
type Manager struct {
	cfg           Config
	logger        *slog.Logger
	reporter      Reporter
	eventReporter EventReporter
	flushReporter FlushReporter
	eventSource   EventSource
	// inflight counts a tick being walked and every report it started
	// until the report returned (internal/inflight SubscriptionPending):
	// the group the clock carries, nil outside a fabrictest site.
	inflight *inflight.Group

	// icdIdleMax is the IdleModeDuration of a LIT-capable ICD publisher,
	// seconds; zero for a node that is no such ICD.
	icdIdleMax atomic.Uint32

	// mu guards byID, eventReporter, flushReporter, eventSource, nextID, onClosed,
	// onTerminated and perFabric.
	mu        sync.RWMutex
	byID      map[uint32]*Subscription
	perFabric map[uint8]int
	nextID    uint32
	// admissions counts the subscriptions admitted, the source of
	// [Subscription.admitted].
	admissions uint64
	onClosed   func(subID uint32)
	// onSessionChanged observes a change of one session's subscription
	// set: a subscription added to it or closed on it.
	onSessionChanged func(sessionID uint16)
	// onTerminated observes the subset of closes that end a subscription
	// for good — see [Manager.SetOnSubscriptionTerminated].
	onTerminated func(subID uint32, fabricIndex uint8)

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// SetICDIdleModeDuration makes the manager a LIT-capable ICD publisher:
// every subscription from now on is granted the ICD's IdleModeDuration as
// its MaxInterval — its full idle cycle — floored at the subscriber's
// MinIntervalFloor, whatever ceiling the subscriber requested and without
// the publisher limit (IdleModeDuration is always within the §2.11 ICD
// publisher limit MAX(IdleModeDuration, 60 min)). Zero restores the
// generic negotiation. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:302-312
// (#icdIdleMaxInterval) and chip's ReadHandler.
func (m *Manager) SetICDIdleModeDuration(seconds uint16) { m.icdIdleMax.Store(uint32(seconds)) }

// SetEventReporter wires the optional event-drain hook. Pass nil to
// detach. Safe to call before Start; calling after Start swaps in the
// new hook for subsequent ticks.
func (m *Manager) SetEventReporter(r EventReporter) {
	m.mu.Lock()
	m.eventReporter = r
	m.mu.Unlock()
}

// SetFlushReporter wires the hook [Manager.FlushFabric] reports through.
// Pass nil to detach; without one a flush sends nothing and leaves what
// the subscriptions owe in place. Safe to call before Start.
func (m *Manager) SetFlushReporter(r FlushReporter) {
	m.mu.Lock()
	m.flushReporter = r
	m.mu.Unlock()
}

// SetEventSource wires the log the engine reads a subscription's owed
// events from when its report is due. Pass nil to detach; without a
// source the owed events are dropped at drain. Safe to call at any time.
func (m *Manager) SetEventSource(src EventSource) {
	m.mu.Lock()
	m.eventSource = src
	m.mu.Unlock()
}

func (m *Manager) eventSourceOf() EventSource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.eventSource
}

// SetOnSubscriptionClosed registers a callback fired once per
// subscription the manager terminates, whichever close path got there.
// The transport side keeps its own per-subscription routing state that
// only a fresh SubscribeRequest recreates, so without this hook a
// cleanly closed subscription leaks that state for the daemon's
// lifetime. Pass nil to detach.
func (m *Manager) SetOnSubscriptionClosed(fn func(subID uint32)) {
	m.mu.Lock()
	m.onClosed = fn
	m.mu.Unlock()
}

// SetOnSubscriptionTerminated registers a callback fired once per
// subscription that is closed *for good*: the peer replaced it
// (KeepSubscriptions=false, [Manager.ClosePeer], a same-session
// re-subscribe), the peer rejected or stopped acknowledging its reports
// ([Manager.Close]), its endpoint or fabric went away
// ([Manager.CloseEndpoint], [Manager.CloseFabric]).
//
// A subscription that ends because its session closed
// ([Manager.CloseSession], [Manager.CloseFabricExcept]) or that the
// transport released without the peer having said anything
// ([Manager.Release]) is closed but NOT terminated, and this hook stays
// silent. That is the line matter.js draws between
// `ServerSubscription.isTerminated` and a plain close
// (packages/node/src/node/server/ServerSubscription.ts:handlePeerCancel,
// #sendUpdate, #sendUpdateMessage), and the one
// SubscriptionsServer.#subscriptionCancelled
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts)
// keys persistence on: a terminated subscription is forgotten, a closed
// one is re-established after a restart.
//
// The second argument is the subscription's accessing fabric
// ([Subscription.FabricIndex]). The host starts that fabric's
// "reconnect" broadcast with it, as matter.js's subscriptionsChanged
// observer does when the subscription isTerminated
// (packages/protocol/src/protocol/DeviceAdvertiser.ts:174-179 at
// 862ad75f). Pass nil to detach.
func (m *Manager) SetOnSubscriptionTerminated(fn func(subID uint32, fabricIndex uint8)) {
	m.mu.Lock()
	m.onTerminated = fn
	m.mu.Unlock()
}

// SetOnSessionSubscriptionsChanged registers a callback fired after a
// session's set of subscriptions changed — a subscription added on it, or
// one closed on it by any close path — with the session's id, outside the
// manager's lock. matter.js's SessionManager raises subscriptionsChanged
// for both (packages/node/src/behavior/system/sessions/
// SessionsBehavior.ts:53, :75-93 at 862ad75f). Pass nil to detach.
func (m *Manager) SetOnSessionSubscriptionsChanged(fn func(sessionID uint16)) {
	m.mu.Lock()
	m.onSessionChanged = fn
	m.mu.Unlock()
}

// finishClose runs the per-subscription teardown for every victim a
// close path removed from the tables. Callers must have released m.mu:
// the observer reaches back into the transport layer. terminated says
// whether the close ends the subscription for good (see
// [Manager.SetOnSubscriptionTerminated]).
func (m *Manager) finishClose(victims []*Subscription, terminated bool) {
	m.mu.RLock()
	hook := m.onClosed
	termHook := m.onTerminated
	changed := m.onSessionChanged
	m.mu.RUnlock()
	for _, sub := range victims {
		sub.Close()
		if hook != nil {
			hook(sub.ID)
		}
		if terminated && termHook != nil {
			termHook(sub.ID, sub.FabricIndex)
		}
		if changed != nil {
			changed(sub.SessionID)
		}
	}
}

// NewManager constructs a manager. logger and reporter may be nil;
// nil reporter means subscriptions are tracked but no reports flow
// out (useful for tests of the cadence math alone).
func NewManager(cfg Config, reporter Reporter, logger *slog.Logger) *Manager {
	if cfg.MaxSubscriptionsPerFabric == 0 {
		cfg.MaxSubscriptionsPerFabric = DefaultMaxSubscriptionsPerFabric
	}
	if cfg.MaxIntervalCeilingSeconds == 0 {
		cfg.MaxIntervalCeilingSeconds = DefaultMaxIntervalCeilingSeconds
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = DefaultTickInterval
	}
	cfg.Clock = clock.Or(cfg.Clock)
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:       cfg,
		logger:    logger,
		reporter:  reporter,
		inflight:  inflight.Of(cfg.Clock),
		byID:      make(map[uint32]*Subscription),
		perFabric: make(map[uint8]int),
		// SubscriptionId is a uint32 carried on every ReportData chunk
		// and on the SubscribeResponse. matter.js Sample uses a random
		// uint32 (observed: 1561508961 / 5f7d1e3c hex) — Apple's
		// ReadClient (`src/app/ReadClient.cpp:643`) reads the id from
		// the first priming report, stores it, and matches every
		// follow-up. A uniform low value `1` across every Subscribe
		// looks like a placeholder / uninitialised state; randomising
		// the seed avoids that pattern.
		nextID: randomSubscriptionIDStart(),
		stopCh: make(chan struct{}),
	}
}

// randomSubscriptionIDStart returns a uniformly-random uint32 seed for
// the subscription-id allocator. Zero is reserved by the IM layer for
// "absent"; we re-roll until non-zero.
func randomSubscriptionIDStart() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// invariant: crypto/rand failure at process start is a host-level
		// entropy-source fault, not reachable via any remote input —
		// surface as panic so the operator notices, rather than
		// silently regressing to the predictable starting id that
		// reproduces the Apple cache-drop symptom.
		panic("matter/subscription: crypto/rand.Read failed: " + err.Error())
	}
	id := binary.BigEndian.Uint32(b[:])
	if id == 0 {
		id = 1
	}
	return id
}

// Start launches the engine tick goroutine. Idempotent.
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		m.wg.Add(1)
		go m.run(ctx)
	})
}

// Stop terminates the engine and waits for it to drain: the tick loop and
// every report it started. The context those reports' reporters hold is
// cancelled first, so a reporter that honours it does not hold Stop.
// Idempotent.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.wg.Wait()
}

// Subscribe accepts a SubscribeRequest and returns the live record.
// The caller wires the [Subscription.ID] into the SubscribeResponse.
func (m *Manager) Subscribe(req SubscribeArgs) (*Subscription, error) {
	if err := m.validateCadence(req.MinIntervalFloor, req.MaxIntervalCeiling); err != nil {
		return nil, err
	}

	// When the caller flags that this request is a replacement (same session
	// re-subscribing), evict any prior subscription on the same session before
	// the quota check so that the new one fits even when the old entry still
	// holds a slot.
	if req.ReplaceSessionDuplicate && req.SessionID != 0 {
		// The peer replaced these — terminated, not merely closed. One
		// still priming is not the peer's yet (ClosePeer).
		m.closeSessionEstablished(req.SessionID)
	}

	m.mu.Lock()
	var evicted []*Subscription
	if m.perFabric[req.FabricIndex] >= m.cfg.MaxSubscriptionsPerFabric {
		if !req.EvictAtQuota {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: fabric=%d quota=%d", ErrFabricQuotaExceeded, req.FabricIndex, m.cfg.MaxSubscriptionsPerFabric)
		}
		evicted = m.evictOldestLocked(req.FabricIndex)
	}
	id := m.allocateIDLocked()
	// MaxInterval publisher selection semantics (chip alignment):
	// Matter §10.6.3.2 requires the publisher's MaxInterval in SubscribeResponse
	// to be ≤ the subscriber's requested MaxInterval. chip (non-ICD path,
	// `src/app/ReadHandler.cpp:769-809`) accepts the subscriber's MaxInterval
	// directly and bounds it only by `kSubscriptionMaxIntervalPublisherLimit`
	// (3600 s). matter.js (`packages/node/src/node/server/ServerSubscription.ts:269-282`)
	// further clamps to min(subscriptionMaxInterval, maxIntervalCeiling) and
	// also lifts by minIntervalFloor. go-fabric clamps maxCeil down to
	// cfg.MaxIntervalCeilingSeconds (default 3600 s, matching chip) and
	// minFloor up to cfg.MinIntervalFloorSeconds (default 0: the request
	// as it is). The post-clamp inversion
	// check (ErrCadenceInvertedAfterClamp) rejects requests where the floored
	// minimum exceeds the capped ceiling — equivalent to matter.js's lower-bound
	// guarantee. The negotiated MaxIntervalCeiling stored on the Subscription is
	// what the bridge advertises in SubscribeResponse, satisfying §10.6.3.2.
	// The cap never takes the ceiling below the subscriber's own floor:
	// MaxInterval stays >= MinIntervalFloor even when the floor exceeds
	// the publisher limit (Matter §8.5.3.2). Mirrors matter.js
	// packages/node/src/node/server/ServerSubscription.ts:324-343 at
	// 862ad75f (Duration.max(minIntervalFloor, …) around the
	// MAX_INTERVAL_PUBLISHER_LIMIT cap) and chip ReadHandler.cpp, which
	// accepts the requested MaxInterval; such a request was refused as
	// inverted after the clamp.
	maxCeil := max(min(req.MaxIntervalCeiling, m.cfg.MaxIntervalCeilingSeconds), req.MinIntervalFloor)
	if idle := uint16(m.icdIdleMax.Load()); idle > 0 { //nolint:gosec // G115: SetICDIdleModeDuration stores a uint16
		maxCeil = max(req.MinIntervalFloor, idle)
	}
	minFloor := max(req.MinIntervalFloor, m.cfg.MinIntervalFloorSeconds)
	// Re-validate AFTER clamp: the configured ceiling may be below the
	// floored minimum, producing a silent inversion that permanently
	// blocks the change report. Reject early so callers can surface
	// a SubscribeResponse.Status = CONSTRAINT_ERROR rather than silently
	// delivering a subscription that never reports dirty changes.
	// matter.js ref: packages/node/src/node/server/ServerSubscription.ts
	// — cadence validation runs on negotiated values.
	if minFloor > maxCeil {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: min=%d > max=%d (after clamp)", ErrCadenceInvertedAfterClamp, minFloor, maxCeil)
	}
	sub := &Subscription{
		ID:                 id,
		FabricIndex:        req.FabricIndex,
		PeerNodeID:         req.PeerNodeID,
		SessionID:          req.SessionID,
		MinIntervalFloor:   minFloor,
		MaxIntervalCeiling: maxCeil,
		KeepSubscriptions:  req.KeepSubscriptions,
		AttributePaths:     append([]im.ConcreteAttributePath(nil), req.AttributePaths...),
		EventPaths:         append([]im.ConcreteEventPath(nil), req.EventPaths...),
		// Stamp lastReport=now at admission so the very first engine tick
		// after Subscribe() does not see [Subscription.lastReport.IsZero]
		// and fire an immediate keep-alive. The bridge follows up with
		// TouchLastReport after the initial-report-stream + SubscribeResponse
		// has shipped, but the 250 ms engine tick can fire BETWEEN
		// Subscribe() and that TouchLastReport call — the bridge takes
		// ~100 ms to chunk + send + per-chunk-wait + dispatch the
		// SubscribeResponse. The empty 52-byte keep-alive ReportData the
		// engine emits in that window arrives at the controller before
		// it has piggyback-acked the initial report, and chip-tool's
		// MRP layer drops it with `Dropping message without piggyback
		// ack when we are waiting for an ack`, eventually timing out
		// the entire subscription (chip-tool-test-brief T7/T8). Stamping
		// here closes the race; TouchLastReport remains a no-op refresh.
		lastReport: m.cfg.Clock.Now(),
		priming:    req.Priming,
	}
	m.admitLocked(sub)
	changed := m.onSessionChanged
	m.mu.Unlock()
	m.finishClose(evicted, true)
	if changed != nil {
		changed(req.SessionID)
	}

	m.logger.Debug(
		"im.subscription.added",
		slog.Uint64("subscription_id", uint64(id)),
		slog.Int("fabric_index", int(req.FabricIndex)),
		slog.Int("min", int(minFloor)),
		slog.Int("max", int(maxCeil)),
		slog.Int("paths", len(req.AttributePaths)),
	)
	return sub, nil
}

// admitLocked enters sub in the tables as the latest admitted. Callers
// hold m.mu.
func (m *Manager) admitLocked(sub *Subscription) {
	m.admissions++
	sub.admitted = m.admissions
	m.byID[sub.ID] = sub
	m.perFabric[sub.FabricIndex]++
}

// evictOldestLocked removes the oldest subscription of fabricIndex from
// the tables and returns it for [Manager.finishClose] — connectedhomeip's
// eviction of the requesting fabric's subscription when its own share is
// what the new one needs (src/app/InteractionModelEngine.cpp:1341-1350 at
// 6170af84, TrimFabricForSubscriptions with aForceEvict, :1173-1247): the
// candidate is the fabric's oldest (:1223-1230), one using more than the
// fabric's share of the path pool first (:1215-1222) — a pool this module
// does not keep, so no subscription here is over it. A subscription still
// priming is a candidate as there: connectedhomeip walks every
// subscribe-type handler of the fabric (:1198-1202). Callers hold m.mu.
func (m *Manager) evictOldestLocked(fabricIndex uint8) []*Subscription {
	var oldest *Subscription
	for _, sub := range m.byID {
		if sub.FabricIndex != fabricIndex {
			continue
		}
		if oldest == nil || sub.admitted < oldest.admitted {
			oldest = sub
		}
	}
	if oldest == nil {
		return nil
	}
	delete(m.byID, oldest.ID)
	m.perFabric[fabricIndex]--
	if m.perFabric[fabricIndex] <= 0 {
		delete(m.perFabric, fabricIndex)
	}
	m.logger.Debug("im.subscription.evicted",
		slog.Uint64("subscription_id", uint64(oldest.ID)),
		slog.Int("fabric_index", int(fabricIndex)))
	return []*Subscription{oldest}
}

// Get returns the subscription for id.
func (m *Manager) Get(id uint32) (*Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return sub, nil
}

// Close terminates a single subscription: the peer rejected it, or stopped
// acknowledging its reports. Fires [Manager.SetOnSubscriptionTerminated].
func (m *Manager) Close(id uint32) error {
	return m.closeOne(id, true)
}

// Release closes a single subscription without terminating it — the
// transport gave up on it without the peer having cancelled it (matter.js
// closes a subscription on a non-InvalidSubscription error status without
// setting `isTerminated`, ServerSubscription.ts:#sendUpdateMessage), or a
// re-establishment attempt that never became active is unwound. The
// terminated hook stays silent.
func (m *Manager) Release(id uint32) error {
	return m.closeOne(id, false)
}

func (m *Manager) closeOne(id uint32, terminated bool) error {
	m.mu.Lock()
	sub, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.byID, id)
	m.perFabric[sub.FabricIndex]--
	if m.perFabric[sub.FabricIndex] <= 0 {
		delete(m.perFabric, sub.FabricIndex)
	}
	m.mu.Unlock()
	m.finishClose([]*Subscription{sub}, terminated)
	return nil
}

// CloseSession closes every subscription tied to sessionID. Used when an
// operational session disappears. The subscriptions are closed, not
// terminated: a session that went away (peer close, idle reap, shutdown)
// says nothing about whether the peer still wants them, so a persisted
// record survives (matter.js SubscriptionsServer keeps a cancelled but
// not terminated subscription).
func (m *Manager) CloseSession(sessionID uint16) {
	m.closeSession(sessionID, false)
}

// closeSessionEstablished terminates the subscriptions of sessionID that
// are no longer priming — the replacement [SubscribeArgs.ReplaceSessionDuplicate]
// asks for, which leaves one still priming as [Manager.ClosePeer] does.
func (m *Manager) closeSessionEstablished(sessionID uint16) {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if sub.SessionID == sessionID && !sub.isPriming() {
			victims = append(victims, sub)
			delete(m.byID, id)
			m.perFabric[sub.FabricIndex]--
			if m.perFabric[sub.FabricIndex] <= 0 {
				delete(m.perFabric, sub.FabricIndex)
			}
		}
	}
	m.mu.Unlock()
	m.finishClose(victims, true)
}

func (m *Manager) closeSession(sessionID uint16, terminated bool) {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if sub.SessionID == sessionID {
			victims = append(victims, sub)
			delete(m.byID, id)
			m.perFabric[sub.FabricIndex]--
			if m.perFabric[sub.FabricIndex] <= 0 {
				delete(m.perFabric, sub.FabricIndex)
			}
		}
	}
	m.mu.Unlock()
	m.finishClose(victims, terminated)
}

// ClosePeer terminates every subscription owned by the (fabric, peer)
// tuple. Returns the number of subscriptions cleared, so callers can
// log a meaningful diagnostic.
//
// Mirrors matter.js packages/node/src/node/server/InteractionServer.ts:
// 647-665 at db021da0 — when a SubscribeRequest on a session with a
// fabric carries `KeepSubscriptions=false`, every existing subscription
// from the same peer (matched on PeerAddress = (fabric, peerNodeId)) is
// cancelled before the new subscription is admitted.
//
// A subscription still priming is left alone: it is not the peer's yet.
// matter.js adds a subscription to its session only when it activates,
// after its SubscribeResponse (ServerSubscription.ts:411-421 at db021da0),
// and the teardown walks the sessions' subscriptions
// (InteractionServer.ts:647-662), so a controller that subscribes twice
// at once keeps the first; closing it here ended its priming report
// under it (review II, R2-P1-req2-01 follow-on).
func (m *Manager) ClosePeer(fabricIndex uint8, peerNodeID uint64) int {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if sub.FabricIndex == fabricIndex && sub.PeerNodeID == peerNodeID && !sub.isPriming() {
			victims = append(victims, sub)
			delete(m.byID, id)
			m.perFabric[fabricIndex]--
			if m.perFabric[fabricIndex] <= 0 {
				delete(m.perFabric, fabricIndex)
			}
		}
	}
	m.mu.Unlock()
	m.finishClose(victims, true)
	return len(victims)
}

// CloseFabric terminates every subscription on fabricIndex. Used
// when OperationalCredentials.RemoveFabric fires.
func (m *Manager) CloseFabric(fabricIndex uint8) {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if sub.FabricIndex == fabricIndex {
			victims = append(victims, sub)
			delete(m.byID, id)
		}
	}
	delete(m.perFabric, fabricIndex)
	m.mu.Unlock()
	m.finishClose(victims, true)
}

// FlushFabric reports what the fabric's subscriptions owe — the pending
// events and the dirty attributes, together in one report per
// subscription — now, the min interval and a deferral notwithstanding,
// and returns once the reports are handed to the wire and answered or
// given up; the subscriptions of exceptSessionID are left out (0 excepts
// none — what the bridge passes, the session carrying RemoveFabric
// included, R2-P1-req2-04). It is
// the flush of matter.js's Fabric.leave before the fabric goes
// (packages/protocol/src/fabric/Fabric.ts:447-457 at db021da0): every
// session of the fabric runs `closeSubscriptions(true)`
// (NodeSession.ts:312-323), each subscription closing with a flush through
// the session (ServerSubscription.ts:753-763 #flush, one
// #triggerSendUpdate), so the Leave event the node emitted a moment
// before reaches the subscribers while the fabric still admits them. A
// subscription whose report is in flight carries what it drained and is
// skipped; a priming one owes nothing yet. Without a [FlushReporter]
// nothing is drained or sent.
//
// Unlike matter.js, whose subscription is marked closed before its flush
// (ServerSubscription.ts:776-788 #close, :800-809 #markClosed),
// FlushFabric leaves the subscriptions open; they close with their
// sessions right after, when the fabric is removed. In that window only
// an ongoing report can go out, and none waits for its answer.
func (m *Manager) FlushFabric(ctx context.Context, fabricIndex uint8, exceptSessionID uint16) {
	m.flush(ctx, func(sub *Subscription) bool {
		return sub.FabricIndex == fabricIndex && sub.SessionID != exceptSessionID
	})
}

// FlushSession is [Manager.FlushFabric] for the subscriptions of one
// session: it reports what they owe and returns once the reports are
// answered or given up, leaving the subscriptions open to close with
// their session. It is the flush of matter.js's rollover close
// (packages/protocol/src/session/NodeSession.ts:406-410 at db021da0,
// `closeSubscriptions(true)` before the session closes; NodeSession.ts
// :312-323) (review II, R2-P2-dr-req-07).
func (m *Manager) FlushSession(ctx context.Context, sessionID uint16) {
	m.flush(ctx, func(sub *Subscription) bool { return sub.SessionID == sessionID })
}

// flush reports what every open, primed subscription selected by match
// owes, one report each, and waits for the reports.
func (m *Manager) flush(ctx context.Context, match func(*Subscription) bool) {
	m.mu.RLock()
	flushReporter := m.flushReporter
	m.mu.RUnlock()
	if flushReporter == nil {
		return
	}
	now := m.cfg.Clock.Now()
	var wg sync.WaitGroup
	for _, sub := range m.snapshot() {
		if !match(sub) || sub.IsClosed() || sub.isPriming() {
			continue
		}
		if !m.claimReport(ctx, sub) {
			continue
		}
		work, due := sub.drainDue(now, m.eventSourceOf(), true)
		if !due {
			sub.reporting.Store(false)
			continue
		}
		events, dirty := work.events, work.paths
		wg.Add(1)
		m.inflight.Enter(inflight.SubscriptionPending)
		go func() {
			defer wg.Done()
			defer m.inflight.Leave(inflight.SubscriptionPending)
			defer sub.reporting.Store(false)
			flushReporter(ctx, sub, events, dirty)
		}()
	}
	wg.Wait()
}

// claimReport takes sub's reporting flag for a flush. A report the
// engine is sending at that moment drained the queues before the data
// the flush owes arrived, so the flush waits for it to finish instead of
// skipping the subscription; it gives up when sub closes or ctx ends.
// The wait is paced on the manager's clock.
func (m *Manager) claimReport(ctx context.Context, sub *Subscription) bool {
	for !sub.reporting.CompareAndSwap(false, true) {
		if sub.IsClosed() {
			return false
		}
		timer := m.cfg.Clock.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C():
		}
	}
	return true
}

// CloseFabricExcept closes every subscription on fabricIndex except those
// bound to exceptSessionID. They are closed with their sessions, not
// terminated (see [Manager.SetOnSubscriptionTerminated]). UpdateNOC tears down the rotated fabric's other
// CASE sessions but must preserve the invoking session (and the subscriptions
// it carries) so its NOCResponse reaches the wire and it can re-CASE. Mirrors
// chip FabricTable::AbortAllOtherCommunicationOnFabric pinning the invoking
// session. exceptSessionID==0 terminates every subscription on the fabric.
func (m *Manager) CloseFabricExcept(fabricIndex uint8, exceptSessionID uint16) {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if sub.FabricIndex == fabricIndex && sub.SessionID != exceptSessionID {
			victims = append(victims, sub)
			delete(m.byID, id)
			m.perFabric[fabricIndex]--
		}
	}
	if m.perFabric[fabricIndex] <= 0 {
		delete(m.perFabric, fabricIndex)
	}
	m.mu.Unlock()
	m.finishClose(victims, false)
}

// CloseEndpoint terminates every subscription that contains at least
// one AttributePath or EventPath targeting endpointID. Called after a
// topology Reassemble removes an endpoint so that the engine stops
// ticking subscriptions that can never match a live cluster again.
//
// Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts
// lifecycle: when a BridgedNode endpoint is
// removed from the Aggregator the outer bridge calls
// `endpoint.lifecycle.remove()` which causes any subscription targeting
// that endpoint to be closed by the InteractionServer.
func (m *Manager) CloseEndpoint(endpointID uint16) int {
	m.mu.Lock()
	victims := make([]*Subscription, 0)
	for id, sub := range m.byID {
		if subReferencesEndpoint(sub, endpointID) {
			victims = append(victims, sub)
			delete(m.byID, id)
			m.perFabric[sub.FabricIndex]--
			if m.perFabric[sub.FabricIndex] <= 0 {
				delete(m.perFabric, sub.FabricIndex)
			}
		}
	}
	m.mu.Unlock()
	m.finishClose(victims, true)
	return len(victims)
}

// subReferencesEndpoint reports whether sub has any AttributePath or
// EventPath whose Endpoint == endpointID.
func subReferencesEndpoint(sub *Subscription, endpointID uint16) bool {
	for _, p := range sub.AttributePaths {
		if p.HasEndpoint && p.Endpoint == endpointID {
			return true
		}
	}
	for _, p := range sub.EventPaths {
		if p.HasEndpoint && p.Endpoint == endpointID {
			return true
		}
	}
	return false
}

// Active returns the count of live subscriptions.
func (m *Manager) Active() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID)
}

// SubjectHasActiveSubscription reports whether the given (fabricIndex,
// peerNodeID) pair holds at least one active subscription. A "subject"
// in the Matter sense is the (fabric, subject/peer) identity — the same
// concept used by the AccessControl cluster's SubjectID field.
//
// This is a diagnostic helper that mirrors chip's
// src/app/ReadHandler.cpp SubjectHasActiveSubscription semantics, used
// by the IM engine to decide whether to emit a Busy status when a
// subscription request arrives from a peer that is already subscribed
// at quota.
func (m *Manager) SubjectHasActiveSubscription(fabricIndex uint8, peerNodeID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sub := range m.byID {
		if sub.FabricIndex == fabricIndex && sub.PeerNodeID == peerNodeID {
			return true
		}
	}
	return false
}

// FabricHasAtLeastOneActiveSubscription reports whether fabricIndex
// holds any active subscription. Mirrors chip's
// src/app/ReadHandler.cpp FabricHasAtLeastOneActiveSubscription, used
// by the IM engine to determine whether a fabric is still "in use"
// before accepting a KeepSubscriptions=false request that would evict
// all subscriptions for that fabric.
func (m *Manager) FabricHasAtLeastOneActiveSubscription(fabricIndex uint8) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.perFabric[fabricIndex] > 0
}

// OnAttributeChanged notifies every subscription whose path-set
// covers `path` that a new value is available. The engine emits the
// report on the next tick once MinInterval has elapsed; the events the
// subscription owes go out in it.
func (m *Manager) OnAttributeChanged(path im.ConcreteAttributePath) {
	// A changesOmitted ("C") attribute is never reported on change, however
	// the change was made — a write, a command, the server itself (matter.js
	// ProtocolService.addCluster leaves it out of the changed set). It still
	// appears in priming reports and reads. TC-ACE-1.6 counts the
	// GroupKeyMap reports a subscription receives and expects none.
	if path.HasCluster && path.HasAttribute && schema.AttributeChangesOmitted(path.Cluster, path.Attribute) {
		return
	}
	m.mu.RLock()
	matches := make([]*Subscription, 0)
	for _, sub := range m.byID {
		for _, p := range sub.AttributePaths {
			if pathMatches(p, path) {
				matches = append(matches, sub)
				break
			}
		}
	}
	m.mu.RUnlock()
	for _, sub := range matches {
		sub.markDirty(path)
	}
}

// DeferReports holds off the next report of every subscription, of any
// session or fabric, that selects one of endpoints, by delay: matter.js
// InteractionServer #deferReports for an InvokeRequest's DelayReportData
// (DelayMinMs plus the jitter the caller picked), run before the invoke
// dispatches, whether or not its commands succeed.
func (m *Manager) DeferReports(endpoints map[uint16]bool, delay time.Duration) {
	if len(endpoints) == 0 {
		return
	}
	now := m.cfg.Clock.Now()
	for _, sub := range m.snapshot() {
		if sub.selectsAnyEndpoint(endpoints) {
			sub.DeferReports(now, delay)
		}
	}
}

// SubscribeArgs is the input to [Manager.Subscribe].
type SubscribeArgs struct {
	FabricIndex        uint8
	PeerNodeID         uint64
	SessionID          uint16
	MinIntervalFloor   uint16
	MaxIntervalCeiling uint16
	KeepSubscriptions  bool
	AttributePaths     []im.ConcreteAttributePath
	EventPaths         []im.ConcreteEventPath
	// Priming admits the subscription in its priming state: the engine
	// sends it nothing — no change report, no keep-alive — until the
	// caller has sent the priming report and the SubscribeResponse and
	// calls [Subscription.EndPriming]. A report on a subscription the
	// controller has not seen established yet is answered
	// INVALID_SUBSCRIPTION and ends it; a priming report long enough to
	// outlast the keep-alive interval used to provoke exactly that (the
	// CHIP harness's TC-IDM-4.3 wildcard subscription runs 37 chunks).
	Priming bool
	// ReplaceSessionDuplicate instructs Subscribe to close any
	// existing subscriptions tied to the same SessionID before
	// admitting the new one. Set this when a CASE session sends a
	// second SubscribeRequest while a subscription from its previous
	// attempt on the same session is still live — keeping the old
	// entry would accumulate stale subscriptions and waste per-fabric
	// quota. When false (the default) existing subscriptions on the
	// session are left untouched, which is the correct behaviour for
	// a peer that genuinely intends to hold multiple simultaneous
	// subscriptions with different path sets.
	ReplaceSessionDuplicate bool
	// EvictAtQuota admits the subscription when its fabric holds
	// [Config.MaxSubscriptionsPerFabric] subscriptions already, by ending
	// the fabric's oldest — terminated, as [Manager.Close] ends one —
	// instead of refusing it with [ErrFabricQuotaExceeded]. It is
	// connectedhomeip's rule for a SubscribeRequest at the limit
	// (src/app/InteractionModelEngine.cpp:1249-1360 at 6170af84,
	// EnsureResourceForSubscription): a request is not refused for want of
	// room, the requesting fabric's own subscriptions make it
	// (:1341-1350). connectedhomeip applies it only to a subject that may
	// read one of the paths it asks for — the access check precedes the
	// resource check (:861-875) — so the caller sets it only for such a
	// request.
	EvictAtQuota bool
}

// EventFiring is the payload of an [Manager.OnEventFired] call. The
// caller (typically a cluster server when its emit-API fires)
// supplies the cluster-native event Data; the manager fans it out to
// every subscription whose EventPaths cover the (endpoint, cluster,
// event) triple.
type EventFiring struct {
	// Path identifies the concrete event. HasEndpoint / HasCluster /
	// HasEvent must all be true on this side — this is a firing, not
	// a subscription.
	Path im.ConcreteEventPath
	// Number is the monotonic event number per cluster instance.
	Number uint64
	// Priority is the event's priority. It does not decide when the
	// event is reported — the subscriber's urgent paths do — and the
	// report reads the priority it carries from the event log's record.
	Priority im.EventPriority
	// The timestamp and the payload are not carried: the report reads
	// the event log's record for Number (see [EventSource]).
}

// OnEventFired notifies every subscription whose EventPaths cover the
// event path. Each matching subscription records only that it owes
// events from Number on; the engine reads them from the [EventSource]
// when the subscription's next report is due, so the event must already
// be in the source when this is called. The event asks for a report only
// of a subscription one of whose urgent paths (IsUrgent) covers it, and
// that report honours MinIntervalFloor; for any other subscription it
// waits for the next report — an attribute change's, an urgent event's or
// the send interval's. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:385-409 at db021da0.
func (m *Manager) OnEventFired(ev EventFiring) {
	if !ev.Path.HasEndpoint || !ev.Path.HasCluster || !ev.Path.HasEvent {
		return
	}
	type match struct {
		sub    *Subscription
		urgent bool
	}
	m.mu.RLock()
	matches := make([]match, 0)
	for _, sub := range m.byID {
		if selected, urgent := sub.eventUrgency(ev.Path); selected {
			matches = append(matches, match{sub: sub, urgent: urgent})
		}
	}
	m.mu.RUnlock()
	for _, hit := range matches {
		hit.sub.queueEvent(ev.Number, hit.urgent)
	}
}

func (m *Manager) validateCadence(minFloor, maxCeil uint16) error {
	if minFloor > maxCeil {
		return fmt.Errorf("%w: min=%d > max=%d", ErrCadenceOutOfRange, minFloor, maxCeil)
	}
	return nil
}

func (m *Manager) allocateIDLocked() uint32 {
	for {
		id := m.nextID
		m.nextID++
		if m.nextID == 0 {
			m.nextID = 1
		}
		if _, in := m.byID[id]; !in {
			return id
		}
	}
}

// pathMatches reports whether `concrete` falls within the path set
// described by `subscribed`. Wildcards on Endpoint / Cluster /
// Attribute / Node propagate; ListIndex is ignored (the engine emits
// whole-attribute reports, not list-element reports).
func pathMatches(subscribed, concrete im.ConcreteAttributePath) bool {
	if subscribed.HasNode && subscribed.Node != concrete.Node {
		return false
	}
	if subscribed.HasEndpoint && subscribed.Endpoint != concrete.Endpoint {
		return false
	}
	if subscribed.HasCluster && subscribed.Cluster != concrete.Cluster {
		return false
	}
	if subscribed.HasAttribute && subscribed.Attribute != concrete.Attribute {
		return false
	}
	return true
}

// snapshot returns a defensive copy of the live subscription list
// for the engine tick.
//
// Concurrency note: this function acquires RLock, copies the map into a
// slice, and releases the lock before the caller iterates. A concurrent Subscribe or Close
// that races the copy is fine — the caller receives a snapshot of the
// map at that instant; new subscriptions added after the snapshot are
// picked up on the next tick, and removed subscriptions are guarded by
// [Subscription.IsClosed] inside the engine loop. Per-subscription
// mutations (markDirty, drainDue) are
// each protected by [Subscription.mu], so no data race exists. This
// pattern mirrors matter.js's per-subscription timer model where each
// ServerSubscription owns its own timer and there is no shared-map
// iteration (packages/node/src/node/server/ServerSubscription.ts).
func (m *Manager) snapshot() []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Subscription, 0, len(m.byID))
	for _, sub := range m.byID {
		out = append(out, sub)
	}
	return out
}

// Info is one active subscription as [Manager.Snapshot]
// lists it: a copy of the [Subscription]'s request and identity, which a
// host reads without reaching the subscription itself (it cannot close
// it). matter.js persists the same record per subscription in
// SubscriptionsServer's state (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts:149-159
// at 862ad75f: subscriptionId, peerAddress, minIntervalFloor,
// maxIntervalCeiling, attributeRequests, eventRequests, sendInterval).
type Info struct {
	ID          uint32
	FabricIndex uint8
	PeerNodeID  uint64
	SessionID   uint16
	// MinIntervalFloor and MaxIntervalCeiling are the request's, in
	// seconds.
	MinIntervalFloor, MaxIntervalCeiling uint16
	KeepSubscriptions                    bool
	// AttributePaths and EventPaths are the request's paths, wildcards
	// as requested; copies.
	AttributePaths []im.ConcreteAttributePath
	EventPaths     []im.ConcreteEventPath
	// SendInterval is the heartbeat cadence the engine reports at
	// ([Subscription.SendInterval]).
	SendInterval time.Duration
}

// Snapshot lists every subscription the manager holds and has not seen
// closed, by ID: a read-only view for a host's diagnostics. matter.js
// keeps the list as SubscriptionsServer.state.subscriptions
// (SubscriptionsServer.ts:345-354 at 862ad75f) and exposes no live one.
func (m *Manager) Snapshot() []Info {
	subs := m.snapshot()
	out := make([]Info, 0, len(subs))
	for _, sub := range subs {
		if sub == nil || sub.IsClosed() {
			continue
		}
		out = append(out, Info{
			ID: sub.ID, FabricIndex: sub.FabricIndex, PeerNodeID: sub.PeerNodeID, SessionID: sub.SessionID,
			MinIntervalFloor: sub.MinIntervalFloor, MaxIntervalCeiling: sub.MaxIntervalCeiling,
			KeepSubscriptions: sub.KeepSubscriptions,
			AttributePaths:    slices.Clone(sub.AttributePaths), EventPaths: slices.Clone(sub.EventPaths),
			SendInterval: sub.SendInterval(),
		})
	}
	slices.SortFunc(out, func(a, b Info) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// CountBySession returns how many active subscriptions each session
// holds, keyed by session id.
//
// A session with no subscriptions and a controller with several are
// operationally very different states, and neither was visible from
// outside: the manager counted subscriptions only to enforce its
// per-fabric quota. Read-only snapshot; the returned map is the
// caller's.
func (m *Manager) CountBySession() map[uint16]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[uint16]int, len(m.byID))
	for _, sub := range m.byID {
		if sub == nil {
			continue
		}
		out[sub.SessionID]++
	}
	return out
}
