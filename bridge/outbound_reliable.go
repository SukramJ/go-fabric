// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ErrAckWaitTimeout is returned by [outboundReliableTracker.WaitForAck]
// when the deadline expires before the peer ACKs the tracked counter.
// Subscribe-initial chunking uses this to enforce Matter §10.6.6's
// per-chunk handshake: the sender must wait for a `StatusResponse(SUCCESS)`
// (which piggybacks an MRP ACK on the chunk's counter) before shipping
// the next chunk. matter.js mirrors the same flow with
// `await this.waitForSuccess("DataReport")` in
// `InteractionMessenger.ts:721`.
var ErrAckWaitTimeout = errors.New("bridge: outbound ack wait: timeout")

// outboundReliableTracker bookkeeps reliable outbound messages the
// bridge has shipped (ongoing subscription reports today;
// command-style IM responses tomorrow) so an inbound HasAck-bearing
// datagram can clear the corresponding pending counter and a
// scheduler can re-send entries whose ACK never arrived. Independent
// of [mrp.AckTracker] which goes in the opposite direction (we owe
// peer an ACK for an inbound reliable).
//
// The pending map is keyed on (SessionID, MessageCounter). Matter
// §4.12.3 says the ACK carries the acknowledged message's counter in
// the protocol-header AckCounter field, but that counter is only
// unique WITHIN a session — two concurrent sessions each seed their
// MRP counter from an independent random (Matter §4.5.4), so a bare
// counter key lets session A's ACK clear session B's pending entry
// when their counters happen to coincide. Keying on the session too
// mirrors matter.js's per-session ExchangeManager bookkeeping
// (ExchangeManager.ts:340). The inbound ACK path supplies the session
// from the received message header.
type outboundKey struct {
	sessionID uint16
	counter   uint32
}

type outboundReliableTracker struct {
	// mu guards pending, waiters and rtt; it is not held while Tick sends.
	mu      sync.Mutex
	pending map[outboundKey]*outboundEntry
	// unsecured counts the entries of session 0 in pending, which
	// [unsecuredEntryLimit] bounds.
	unsecured int
	// waiters lets callers block on Ack via [WaitForAck] — used by the
	// Subscribe-initial chunked send path so the bridge mirrors
	// matter.js's per-chunk handshake (one ReportData → wait for
	// StatusResponse(SUCCESS) → next ReportData). Closed by [Ack] /
	// [Tick] when the entry is resolved or abandoned.
	waiters map[outboundKey]chan error
	// rtt holds the recent first-try round-trips for diagnostics.
	rtt mrpRTTWindow
	// baseIntervalFor resolves the peer-appropriate MRP base interval
	// for the session (peer-advertised active/idle interval selected
	// by peer activity — matter.js MRP.ts:129). nil, or a session the
	// resolver does not know, falls back to the spec idle default.
	// Set once at construction; read without the mutex.
	baseIntervalFor func(sessionID uint16, now time.Time) time.Duration
	// clock stamps sentWall for the round-trip diagnostic.
	clock clock.Clock
	// fixedBackoff (nanoseconds) pads every wait after the exponential
	// backoff, unamplified: an ICD's fast-poll interval (matter.js
	// MRP.ts:134-139 fixedBackoff, :188; SessionManager
	// localFixedMrpBackoff, set by IcdManagementServer.ts:257). Zero for a
	// node that is no ICD.
	fixedBackoff atomic.Int64
	// wake, when set, wakes the bridge's idle ACK pump once an entry is
	// tracked (Bridge.wakeAckPump). Set once after construction, before
	// the pump starts; read without the mutex.
	wake func()
}

// outboundEntry captures one in-flight reliable datagram and the
// retransmit schedule. The exchangeID is recorded alongside the
// counter so [outboundReliableTracker.AbandonExchange] can drop every
// pending entry of an exchange in one shot — used by the CASE Sigma3
// receive path to stop redundant Sigma2 retransmits once the
// commissioner has progressed past Sigma2. The sessionID feeds the
// per-peer MRP interval selection on every retransmit.
type outboundEntry struct {
	counter    uint32
	sessionID  uint16
	exchangeID uint16
	datagram   []byte
	dest       net.Addr
	retries    int
	nextSendAt time.Time
	// trackedAt is when the entry was tracked, on the schedule's clock; an
	// unsecured-session entry is dropped [unsecuredEntryLifetime] after it.
	trackedAt time.Time
	// margin joins the base interval of every (re)transmission, so the
	// backoff amplifies it: the exchange's additional MRP delay (matter.js
	// MRP.ts retransmissionIntervalOf, `additionalDelay`). Zero for every
	// exchange but a BDX responder's that set one ([TrackWithMargin]).
	margin time.Duration
	// peer, when set, resolves the base interval of a session-0
	// (handshake) reply from the initiator's own SessionParameters and its
	// activity instead of the session resolver, which knows no unsecured
	// session ([outboundReliableTracker.trackUnsecured]).
	peer *handshakePeerTiming
	// sentWall is the tracker's clock reading when the datagram first
	// left, used only to report the peer's round-trip in diagnostics. It is
	// deliberately NOT the injected `now` the retransmit schedule runs on:
	// that is a test seam and may be a fixed instant, which would make
	// every reported round-trip zero. Nothing in the MRP state machine
	// reads this field.
	sentWall time.Time
}

// newOutboundReliableTracker returns a fresh, empty tracker.
// baseIntervalFor may be nil — every send then uses the spec idle
// default as its base interval.
func newOutboundReliableTracker(baseIntervalFor func(sessionID uint16, now time.Time) time.Duration, clk clock.Clock) *outboundReliableTracker {
	return &outboundReliableTracker{
		clock:           clock.Or(clk),
		pending:         make(map[outboundKey]*outboundEntry),
		waiters:         make(map[outboundKey]chan error),
		baseIntervalFor: baseIntervalFor,
	}
}

// baseInterval resolves the MRP base interval for sessionID, falling
// back to the spec SESSION_IDLE_INTERVAL default when no resolver is
// wired (tests) or the session is unknown (session 0 / PASE).
func (t *outboundReliableTracker) baseInterval(sessionID uint16, now time.Time) time.Duration {
	if t.baseIntervalFor != nil {
		if d := t.baseIntervalFor(sessionID, now); d > 0 {
			return d
		}
	}
	return mrp.SessionIdleIntervalDefault
}

// handshakePeerTiming is what the responder knows about the initiator's
// MRP timing while a handshake runs on the unsecured session: the
// intervals the initiator stated in its PBKDFParamRequest / Sigma1
// (spec defaults for the fields it left out) and when it was last heard.
//
// Mirrors matter.js: the responder's UnsecuredSession is created with
// `setActiveTimestamp: !isInitiator` (packages/protocol/src/session/
// UnsecuredSession.ts:30, "When we are the initiator we assume the node
// is in idle mode" — the responder counts its peer active), its timing
// parameters are replaced by the initiator's (PaseServer.ts:153-156,
// CaseServer.ts:120-123 `session.timingParameters =
// initiatorSessionParams`; Session.ts:108-121 stores them), every
// received message refreshes the active timestamp (Session.ts:141-147
// notifyActivity) and retransmissionIntervalOf picks activeInterval while
// the peer is active (MRP.ts:181, isPeerActive: Session.ts:149-151).
type handshakePeerTiming struct {
	idle, active, threshold time.Duration
	// activeAt is when the request this reply answers was received.
	activeAt time.Time
}

// newHandshakePeerTiming resolves the initiator's intervals from params
// (nil when it stated none) with the spec defaults for each field it left
// out; rxAt is when its request arrived.
func newHandshakePeerTiming(params *sessionparams.Parameters, rxAt time.Time) *handshakePeerTiming {
	p := &handshakePeerTiming{
		idle:      mrp.SessionIdleIntervalDefault,
		active:    mrp.SessionActiveIntervalDefault,
		threshold: mrp.SessionActiveThresholdDefault,
		activeAt:  rxAt,
	}
	if params == nil {
		return p
	}
	if params.SessionIdleInterval != 0 {
		p.idle = time.Duration(params.SessionIdleInterval) * time.Millisecond
	}
	if params.SessionActiveInterval != 0 {
		p.active = time.Duration(params.SessionActiveInterval) * time.Millisecond
	}
	if params.SessionActiveThreshold != 0 {
		p.threshold = time.Duration(params.SessionActiveThreshold) * time.Millisecond
	}
	return p
}

// base is the MRP base interval for a (re)transmission at now: the active
// interval while the peer was heard within its active threshold, the idle
// interval after (matter.js MRP.ts:181, re-evaluated per transmission).
func (p *handshakePeerTiming) base(now time.Time) time.Duration {
	if now.Sub(p.activeAt) < p.threshold {
		return p.active
	}
	return p.idle
}

// entryBase is the base interval for the entry's next transmission.
func (t *outboundReliableTracker) entryBase(sessionID uint16, peer *handshakePeerTiming, now time.Time) time.Duration {
	if peer != nil {
		return peer.base(now)
	}
	return t.baseInterval(sessionID, now)
}

// Track records a freshly-sent reliable datagram. counter is the
// MessageCounter we stamped into the message header; dest is the UDP
// peer; datagram is the wire bytes we shipped (defensive-copied so
// caller pools / reuses are safe). Subsequent inbound HasAck with
// AckCounter=counter clears the entry; otherwise the next ACK pump
// tick resends it after the Matter §4.12.6 backoff.
func (t *outboundReliableTracker) Track(counter uint32, sessionID, exchangeID uint16, datagram []byte, dest net.Addr, now time.Time) {
	t.TrackWithMargin(counter, sessionID, exchangeID, datagram, dest, now, 0)
}

// TrackWithMargin is [outboundReliableTracker.Track] with margin added to
// the base interval of every (re)transmission before the backoff
// amplifies it: matter.js MessageExchange #sendAdditionalDelay
// (MessageExchange.ts:1219-1222 at 862ad75f) passed as
// retransmissionIntervalOf's additionalDelay (MRP.ts, `baseInterval =
// (isPeerActive ? activeInterval : idleInterval) + additionalDelay`).
func (t *outboundReliableTracker) TrackWithMargin(counter uint32, sessionID, exchangeID uint16, datagram []byte, dest net.Addr, now time.Time, margin time.Duration) {
	t.track(counter, sessionID, exchangeID, datagram, dest, now, margin, nil)
}

// trackUnsecured tracks a reply on session 0: with the initiator's timing
// when the reply answers its handshake (peer set), on the idle default
// otherwise. Its base interval then comes from peer, not from the session
// resolver, which knows no session 0.
//
// It reports false, and tracks nothing, when the unsecured session already
// holds [unsecuredEntryLimit] replies awaiting their acknowledgement: the
// caller then does not send the reply ([errRetransmitTableFull]).
func (t *outboundReliableTracker) trackUnsecured(counter uint32, exchangeID uint16, datagram []byte, dest net.Addr, now time.Time, peer *handshakePeerTiming) bool {
	return t.track(counter, 0, exchangeID, datagram, dest, now, 0, peer)
}

// unsecuredEntryLimit bounds the replies the unsecured session holds for
// retransmission — the part of the table a peer fills without having
// authenticated: every PBKDFParamResponse, Pake2, Sigma2 and StatusReport
// of a handshake is one, paced by the intervals that peer stated, which
// neither reference clamps (matter.js
// packages/protocol/src/session/SessionIntervals.ts:52-53 at db021da0; chip
// src/protocols/secure_channel/PairingSession.cpp:175-189 at 6170af84).
//
// The number is connectedhomeip's: its retransmission table is
// `ObjectPool<RetransTableEntry, CHIP_CONFIG_RMP_RETRANS_TABLE_SIZE>`
// (src/messaging/ReliableMessageMgr.h:267), sized
// CHIP_CONFIG_MAX_EXCHANGE_CONTEXTS (src/messaging/ReliableMessageProtocolConfig.h:134
// and :146), 16 by default (src/lib/core/CHIPConfig.h:303-305), all at
// 6170af84. A full table fails AddToRetransTable with
// CHIP_ERROR_RETRANS_TABLE_FULL (ReliableMessageMgr.cpp:254-263) and the
// message is neither prepared nor sent (ExchangeMessageDispatch.cpp:63-64);
// the same here. chip's bound covers every session and holds where its
// pools are static — on a platform whose pools live on the heap the size
// parameter is ignored (src/lib/support/Pool.h:619-626; Linux:
// src/platform/Linux/SystemPlatformConfig.h:41). Here it covers the
// unsecured session alone: a secure session's entries belong to an
// authenticated peer and are bounded by its subscriptions and exchanges.
// matter.js has no table: the message awaiting its acknowledgement and its
// timer are fields of the exchange
// (packages/protocol/src/protocol/MessageExchange.ts:285 and :293 at
// db021da0).
const unsecuredEntryLimit = 16

// unsecuredEntryLifetime is how long a reply on the unsecured session may
// wait for its acknowledgement: no handshake outlives it. matter.js gives a
// PASE pairing 60 s (packages/protocol/src/session/pase/PaseServer.ts:33 at
// db021da0, PASE_PAIRING_TIMEOUT — [pasePairingTimeout] here), and a
// per-exchange CASE responder nobody resolved for its TTL is reaped
// ([PerExchangeCaseProvider.StartReaper]; the node facade's is one minute).
// Past it the retransmission would reach a peer whose handshake this node
// can no longer complete. Without the limit an entry stayed until its
// retries were spent, which a peer that states 0xFFFFFFFF ms intervals puts
// months away (review II, R2-P0-bound-A06). The value is
// [pasePairingTimeout]'s, stated here so the limits page can hold it.
const unsecuredEntryLifetime = 60 * time.Second

var (
	// errRetransmitTableFull refuses a reliable reply on the unsecured
	// session that would be the [unsecuredEntryLimit]+1st awaiting its
	// acknowledgement; the reply is not sent.
	errRetransmitTableFull = errors.New("bridge: outbound retransmit table full for the unsecured session")
	// errUnsecuredEntryExpired is [outboundReliableTracker.Tick]'s report
	// of an unsecured-session entry dropped after
	// [unsecuredEntryLifetime].
	errUnsecuredEntryExpired = errors.New("bridge: outbound retransmit: the handshake's time is over")
)

func (t *outboundReliableTracker) track(counter uint32, sessionID, exchangeID uint16, datagram []byte, dest net.Addr, now time.Time, margin time.Duration, peer *handshakePeerTiming) bool {
	cp := make([]byte, len(datagram))
	copy(cp, datagram)
	// The initial wait before the first retransmit uses the full spec
	// formula at transmission 0 — matter.js MRP.ts:129 evaluates the
	// peer-appropriate base interval for every transmission including
	// the first.
	delay := mrp.BackoffDuration(t.entryBase(sessionID, peer, now)+margin, 0, rand.Float64) + time.Duration(t.fixedBackoff.Load())
	t.mu.Lock()
	if sessionID == 0 && t.unsecured >= unsecuredEntryLimit {
		t.mu.Unlock()
		return false
	}
	key := outboundKey{sessionID, counter}
	if _, replaced := t.pending[key]; !replaced && sessionID == 0 {
		t.unsecured++
	}
	t.pending[key] = &outboundEntry{
		counter:    counter,
		sessionID:  sessionID,
		exchangeID: exchangeID,
		datagram:   cp,
		dest:       dest,
		nextSendAt: now.Add(delay),
		trackedAt:  now,
		margin:     margin,
		peer:       peer,
		sentWall:   t.clock.Now(),
	}
	t.mu.Unlock()
	if t.wake != nil {
		t.wake() // after the unlock: the entry is in pending first
	}
	return true
}

// dropLocked removes the entry at key, keeping the unsecured session's
// count; every removal from pending goes through it. Caller holds t.mu.
func (t *outboundReliableTracker) dropLocked(key outboundKey) {
	if _, ok := t.pending[key]; !ok {
		return
	}
	delete(t.pending, key)
	if key.sessionID == 0 {
		t.unsecured--
	}
}

// AbandonExchange drops every pending reliable entry whose
// exchangeID matches and wakes any [WaitForAck] callers blocked on
// those counters with [ErrAckWaitTimeout]. Returns the number of
// entries cleared.
//
// Used by the CASE Sigma3 receive path: once the commissioner has
// progressed past Sigma2, any still-pending Sigma2 retransmits on the
// same exchange are wasted bandwidth at best, and at worst confuse
// the commissioner's MRP layer (Apple iOS drops them with
// `Dropping message without piggyback ack when we are waiting for
// an ack`). Mirrors matter.js's per-exchange cleanup in
// `MessageExchange.ts::close` which discards every retx-pending
// message of the exchange when the receiver advances state.
// Untrack drops a message Track registered whose send then failed: there is
// nothing on the wire to retransmit or to wait for.
func (t *outboundReliableTracker) Untrack(sessionID uint16, counter uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dropLocked(outboundKey{sessionID, counter})
}

func (t *outboundReliableTracker) AbandonExchange(exchangeID uint16) int {
	return t.abandonExchange(exchangeID, false)
}

// AbandonUnsecuredExchange is [outboundReliableTracker.AbandonExchange]
// for the unsecured session alone: the replies of the handshake on
// exchangeID that still wait for their acknowledgement. Called when that
// handshake ends — it failed, timed out, its responder was reaped, or it
// moved past the reply — as connectedhomeip clears an exchange's entry when
// the exchange is aborted (src/messaging/ExchangeContext.cpp:241-246 and
// :277-287 at 6170af84, `DoClose(true)` → `ClearRetransTable(this)`; a
// pairing session that is cleared aborts an exchange still waiting on a
// response, src/protocols/secure_channel/PairingSession.cpp:285-290). An
// entry of a secure session whose peer happened to pick the same exchange
// id stays.
func (t *outboundReliableTracker) AbandonUnsecuredExchange(exchangeID uint16) int {
	return t.abandonExchange(exchangeID, true)
}

func (t *outboundReliableTracker) abandonExchange(exchangeID uint16, unsecuredOnly bool) int {
	t.mu.Lock()
	cleared := 0
	wakers := make([]chan error, 0)
	for key, entry := range t.pending {
		if entry.exchangeID != exchangeID || (unsecuredOnly && key.sessionID != 0) {
			continue
		}
		t.dropLocked(key)
		if ch, has := t.waiters[key]; has {
			delete(t.waiters, key)
			wakers = append(wakers, ch)
		}
		cleared++
	}
	t.mu.Unlock()
	// Wake outside the lock — channel sends may otherwise stall.
	for _, ch := range wakers {
		// Send abandon marker; ignore if no receiver (best-effort).
		select {
		case ch <- ErrAckWaitTimeout:
		default:
			close(ch)
		}
	}
	return cleared
}

// Ack removes the pending entry for (sessionID, acked). Returns true
// when an entry was found, false on stray / duplicate ACK. Wakes any
// [WaitForAck] caller blocked on the same key. sessionID comes from
// the received message header — an ACK is only valid within its own
// session (see the outboundKey rationale).
func (t *outboundReliableTracker) Ack(sessionID uint16, acked uint32) bool {
	key := outboundKey{sessionID, acked}
	t.mu.Lock()
	if _, ok := t.pending[key]; !ok {
		// A stray ACK: the duplicate ack of a retransmission after the
		// entry was cleared. [WaitForAck] installs a waiter only after
		// [Track], so a waiter found here outlived its entry (one
		// [Untrack] dropped without waking it); release it rather than
		// leave it to its deadline.
		if ch, has := t.waiters[key]; has {
			delete(t.waiters, key)
			t.mu.Unlock()
			close(ch)
			return false
		}
		t.mu.Unlock()
		return false
	}
	entry := t.pending[key]
	t.dropLocked(key)
	// Only a first-try datagram yields a usable round-trip. After a
	// retransmit there is no way to tell whether the ACK answers the original
	// or the resend, so timing it from the first send overstates the peer's
	// latency by whole backoff intervals — Karn's algorithm, and the same
	// reason MRP itself never adapts its interval from a retransmitted
	// exchange.
	if entry != nil && entry.retries == 0 && !entry.sentWall.IsZero() {
		t.rtt.record(clock.Since(t.clock, entry.sentWall))
	}
	ch, has := t.waiters[key]
	if has {
		delete(t.waiters, key)
	}
	t.mu.Unlock()
	if has {
		close(ch)
	}
	return true
}

// RTTStats summarises the recent first-try round-trips to Matter controllers.
// Safe on a nil tracker (Matter disabled), which reports an empty summary.
func (t *outboundReliableTracker) RTTStats() MRPRTTStats {
	if t == nil {
		return MRPRTTStats{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rtt.stats()
}

// WaitForAck blocks until the peer ACKs `counter`, the context is
// cancelled, or the entry is abandoned by [Tick] after exceeding the
// retransmit cap. Caller must invoke this AFTER [Track] (otherwise the
// channel is created but never resolved).
//
// Returns nil on ACK; ctx.Err() on context cancellation;
// [ErrAckWaitTimeout] if the deadline elapses without movement; the
// abandon-error (typically [mrp.ErrMaxRetransmissionsReached]) when
// Tick gives up.
func (t *outboundReliableTracker) WaitForAck(ctx context.Context, sessionID uint16, counter uint32) error {
	key := outboundKey{sessionID, counter}
	t.mu.Lock()
	if _, pending := t.pending[key]; !pending {
		t.mu.Unlock()
		return nil // already acked / never tracked
	}
	ch, exists := t.waiters[key]
	if !exists {
		ch = make(chan error, 1)
		t.waiters[key] = ch
	}
	t.mu.Unlock()

	select {
	case err, ok := <-ch:
		if !ok {
			return nil
		}
		return err
	case <-ctx.Done():
		t.mu.Lock()
		// Best-effort cleanup so a cancelled wait doesn't leak the
		// waiter channel forever — the next Ack/abandon for this
		// key would otherwise leave a stranded entry. Only delete
		// our own channel; if the wait raced with another caller
		// installing a fresh chan we don't touch theirs.
		if cur, has := t.waiters[key]; has && cur == ch {
			delete(t.waiters, key)
		}
		t.mu.Unlock()
		return ctx.Err()
	}
}

// Pending reports the count of in-flight entries. Test helper.
func (t *outboundReliableTracker) Pending() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// outboundTickResult enumerates the outcome of a single Tick sweep.
type outboundTickResult struct {
	SessionID  uint16
	ExchangeID uint16
	Counter    uint32
	Err        error // nil = re-sent; mrp.ErrMaxRetransmissionsReached = abandoned
}

// Tick walks every entry whose nextSendAt has elapsed: re-sends via
// `send` (typically `listener.Send`), schedules the next attempt with
// exponential backoff, and gives up after [mrp.MaxRetransmissions].
// Abandoned entries are removed and reported with
// [mrp.ErrMaxRetransmissionsReached] so the caller can drop the
// associated subscription / state.
//
// The due entries are taken and rescheduled under the tracker's lock and
// `send` is called after it is released (ADR 0026 P6): an Ack arriving
// meanwhile removes its entry without waiting for the socket. A send
// error is surfaced via the result slice and counts as the transmission:
// the entry keeps the backoff the sweep scheduled, so a failing socket
// spends the retry budget on the MRP schedule, not at the pump's cadence.
// Mirrors matter.js MessageExchange.ts:905-906 and :975-981 at 862ad75f
// (#retransmitMessage counts the retransmission before it sends, and a
// failed send that is not SessionClosedError still runs
// #initializeResubmission with the computed backoff).
func (t *outboundReliableTracker) Tick(now time.Time, send func(net.Addr, []byte) error) []outboundTickResult {
	type resend struct {
		key      outboundKey
		p        *outboundEntry
		dest     net.Addr
		datagram []byte
	}
	var (
		out  []outboundTickResult
		todo []resend
	)
	t.mu.Lock()
	for key, p := range t.pending {
		// A reply on the unsecured session whose handshake's time is over
		// is dropped, due or not: [unsecuredEntryLifetime].
		if key.sessionID == 0 && now.Sub(p.trackedAt) >= unsecuredEntryLifetime {
			t.dropLocked(key)
			if ch, has := t.waiters[key]; has {
				delete(t.waiters, key)
				ch <- errUnsecuredEntryExpired
				close(ch)
			}
			out = append(out, outboundTickResult{SessionID: key.sessionID, ExchangeID: p.exchangeID, Counter: key.counter, Err: errUnsecuredEntryExpired})
			continue
		}
		if now.Before(p.nextSendAt) {
			continue
		}
		if p.retries >= mrp.MaxRetransmissions {
			t.dropLocked(key)
			if ch, has := t.waiters[key]; has {
				delete(t.waiters, key)
				ch <- mrp.ErrMaxRetransmissionsReached
				close(ch)
			}
			out = append(out, outboundTickResult{SessionID: key.sessionID, ExchangeID: p.exchangeID, Counter: key.counter, Err: mrp.ErrMaxRetransmissionsReached})
			continue
		}
		p.retries++
		// Full spec backoff per Matter §4.12.2.1: peer-appropriate
		// base interval (re-evaluated per retransmission — the peer
		// may have gone idle since the last attempt), margin,
		// exponential growth past the threshold, and jitter. Mirrors
		// matter.js MRP.ts:125-146 retransmissionIntervalOf.
		delay := mrp.BackoffDuration(t.entryBase(p.sessionID, p.peer, now)+p.margin, p.retries, rand.Float64) + time.Duration(t.fixedBackoff.Load())
		p.nextSendAt = now.Add(delay)
		todo = append(todo, resend{key: key, p: p, dest: p.dest, datagram: p.datagram})
	}
	t.mu.Unlock()
	for _, r := range todo {
		// An Ack that arrived since the sweep removed the entry: its
		// retransmission is not sent.
		t.mu.Lock()
		acked := t.pending[r.key] != r.p
		t.mu.Unlock()
		if acked {
			continue
		}
		if err := send(r.dest, r.datagram); err != nil {
			out = append(out, outboundTickResult{SessionID: r.key.sessionID, ExchangeID: r.p.exchangeID, Counter: r.key.counter, Err: err})
			continue
		}
		out = append(out, outboundTickResult{SessionID: r.key.sessionID, Counter: r.key.counter})
	}
	return out
}
