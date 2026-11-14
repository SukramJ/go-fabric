// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/clockvar"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Manager maintains the live set of operational (CASE) sessions and
// brokers resumption-id persistence.
//
// Concurrency: every method is safe for concurrent use. Sessions are
// keyed by Matter session-id (uint16); the manager picks fresh IDs
// from the unused space of [1, 0xFFFE] and reuses the slot when a
// session closes. ID 0 is reserved by Matter for unsecured traffic.
type Manager struct {
	store ResumptionStore

	// clock is the session table's time source (see [Manager.SetClock]);
	// unset is clock.System.
	clock clockvar.Var

	// reaperMu guards reaperStop: the cancel of the running idle reaper
	// ([Manager.StartReaper]), nil when none runs. reaperWG is the
	// reaper goroutine, waited for by [Manager.StopReaper].
	reaperMu   sync.Mutex
	reaperStop context.CancelFunc
	reaperWG   sync.WaitGroup

	// mu guards the session table (sessions, nextID) and the hooks below.
	mu             sync.RWMutex
	sessions       map[uint16]*Entry
	nextID         uint16
	onSessionClose func(sessionID uint16)
	// placeholders are the ids [Manager.AllocateID] staked, oldest first,
	// each with the entry it staked: at a full table the allocator takes
	// the oldest that still stakes its id from the front instead of
	// walking the table ([Manager.allocateIDLocked]). An id that left the
	// table, or holds another entry now, is skipped there and dropped by
	// [Manager.compactPlaceholdersLocked].
	placeholders []placeholderRef
	// onPaseSessionClose fires after onSessionClose for a session
	// established via PASE (SetOnPaseSessionClose).
	onPaseSessionClose func(sessionID uint16)

	// onGracefulClose, when non-nil, fires once per session BEFORE its
	// key material is zeroised on every locally-initiated ("graceful")
	// teardown: idle reap, same-peer stale-CASE eviction, on-demand
	// ClosePeer, and shutdown via [Manager.CloseAllGraceful]. The wire
	// layer wires a best-effort Secure-Channel CloseSession
	// StatusReport sender here so the peer learns the session is gone
	// instead of retransmitting into a void. Mirrors matter.js
	// packages/protocol/src/protocol/ExchangeManager.ts:693
	// (`observers.on(session.gracefulClose, () =>
	// this.#sendCloseSession(session))`) — the session emits
	// gracefulClose while its keys are still live
	// (packages/protocol/src/session/NodeSession.ts:368) and the
	// exchange layer ships the CloseSession report. Deliberately NOT
	// fired for peer-initiated closes ([Manager.Close] — the peer
	// already knows; NodeSession.ts:305 handlePeerClose marks the peer
	// lost so close() skips the gracefulClose emit) nor for the fabric
	// teardown paths (CloseFabric / CloseFabricExcept /
	// ClosePASESessions), where the close is the protocol-visible
	// consequence of a command the peer itself issued.
	onGracefulClose func(sessionID uint16, sess *channel.Session)

	// onReannounce, when non-nil, fires once per distinct (fabric, peer)
	// pair of a teardown sweep that has no remaining live session, with
	// that pair's fabric index — the peer is fully disconnected and the
	// fabric's operational mDNS broadcast should resume so the controller
	// can rediscover + re-CASE the bridge instead of showing it
	// unresponsive. A PASE victim has no fabric and fires index 0: the
	// receiver advertises no operational service for it (matter.js
	// resumes the commissionable advertisement there instead,
	// DeviceAdvertiser.ts:152-161). Mirrors matter.js
	// packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150 at
	// 862ad75f (sessions.deleted, per deleted session: skip when
	// `fabric.hasSessionForPeer(...)`, Fabric.ts:414-420, otherwise
	// `advertisementForFabric(fabric).serviceDisconnected()` for that
	// fabric only, Advertiser.ts:69-75). Not
	// fired by [Manager.CloseAllGraceful]: matter.js kicks off the
	// advertiser shutdown before removing sessions precisely to prevent
	// re-announces during teardown (packages/node/src/behavior/system/
	// network/ServerNetworkRuntime.ts:450-453 at db021da0).
	onReannounce func(fabricIndex uint8)

	// reapExempt, when non-nil, is asked by the idle reaper for every
	// session it found idle; true spares the session
	// ([Manager.SetReapExemption]).
	reapExempt func(sessionID uint16) bool
}

// SetGracefulCloseNotifier registers the wire-layer callback invoked
// once per session before a graceful local teardown zeroises its keys
// — see the [Manager.onGracefulClose] field docs for the exact firing
// matrix and the matter.js provenance. Passing nil clears the hook.
// The callback runs outside the manager's lock and must be fast
// (single best-effort datagram); it MUST NOT call back into APIs that
// mutate the session table.
func (m *Manager) SetGracefulCloseNotifier(fn func(sessionID uint16, sess *channel.Session)) {
	m.mu.Lock()
	m.onGracefulClose = fn
	m.mu.Unlock()
}

// SetReannounceTrigger registers the callback fired, with the fabric
// index, after a teardown sweep leaves a (fabric, peer) pair with zero
// live sessions — see the [Manager.onReannounce] field docs. Passing
// nil clears the hook. The callback runs outside the manager's lock;
// implementations should be non-blocking (fire-and-forget into the
// mDNS layer).
func (m *Manager) SetReannounceTrigger(fn func(fabricIndex uint8)) {
	m.mu.Lock()
	m.onReannounce = fn
	m.mu.Unlock()
}

// SetReapExemption registers the callback the idle reaper
// ([Manager.StartReaper]) asks before it closes a session it found idle:
// true spares the session for this sweep. The bridge wires it to its
// subscription manager, so a session that carries a live subscription is
// never reaped for idleness — the subscriber's MinIntervalFloor can hold
// its reports, and with them the session's only traffic, beyond
// [SessionIdleTimeout] (review II, R2-P2-sess-02, owner decision 43).
// matter.js has no idle reaper (BD-Matter-IdleSessionReaper). The
// callback runs outside the manager's lock; passing nil clears it.
func (m *Manager) SetReapExemption(fn func(sessionID uint16) bool) {
	m.mu.Lock()
	m.reapExempt = fn
	m.mu.Unlock()
}

// notifyGracefulClose dispatches the graceful-close hook for every
// live entry in entries. Caller MUST hold no manager locks and MUST
// invoke this BEFORE closeStaleEntries — the hook encrypts the
// CloseSession StatusReport under the session's still-live keys, the
// same ordering matter.js guarantees by emitting gracefulClose from
// NodeSession.ts:368 close() before the key state is dropped.
//
// A non-zero deadline bounds the sweep: once passed, the remaining
// notifications are skipped so a shutdown never blocks on a slow or
// wedged wire path.
func (m *Manager) notifyGracefulClose(entries []*Entry, deadline time.Time) {
	m.mu.RLock()
	hook := m.onGracefulClose
	m.mu.RUnlock()
	if hook == nil {
		return
	}
	for _, e := range entries {
		if e == nil || e.Session == nil {
			continue
		}
		if !deadline.IsZero() && m.clk().Now().After(deadline) {
			return
		}
		hook(e.SessionID, e.Session)
	}
}

// hasSessionForPeer reports whether any live session matches the
// (fabricIndex, peerNodeID) pair. Mirrors the guard matter.js applies
// before resuming operational broadcast on session deletion
// (DeviceAdvertiser.ts:138 `fabric.hasSessionForPeer(...)`).
func (m *Manager) hasSessionForPeer(fabricIndex uint8, peerNodeID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.sessions {
		if e == nil || e.Session == nil {
			continue
		}
		if e.FabricIndex() == fabricIndex && e.Session.PeerNodeID() == peerNodeID {
			return true
		}
	}
	return false
}

// fireReannounceIfPeerGone fires the reannounce trigger once per
// distinct (fabric, peer) pair among the victims that has no remaining
// live session, passing the pair's fabric index (0 for a PASE victim,
// which has no fabric). Called after the victims are removed from the
// table, so a same-peer replacement session installed by a CASE
// re-establishment naturally suppresses the trigger. Mirrors
// matter.js packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150
// at 862ad75f, which runs the check per deleted session.
func (m *Manager) fireReannounceIfPeerGone(victims []*Entry) {
	m.mu.RLock()
	hook := m.onReannounce
	m.mu.RUnlock()
	if hook == nil {
		return
	}
	type fabricPeer struct {
		fabricIndex uint8
		peerNodeID  uint64
	}
	var gone []fabricPeer
	for _, e := range victims {
		if e == nil || e.Session == nil {
			continue
		}
		p := fabricPeer{e.FabricIndex(), e.Session.PeerNodeID()}
		if slices.Contains(gone, p) {
			continue
		}
		if !m.hasSessionForPeer(p.fabricIndex, p.peerNodeID) {
			gone = append(gone, p)
		}
	}
	for _, p := range gone {
		hook(p.fabricIndex)
	}
}

// SetOnSessionClose registers a callback invoked once per session
// after its slot is removed from the manager. Used by the bridge to
// cascade cleanup into the Interaction-Model subscription manager so
// in-flight subscriptions tied to a now-defunct CASE session are
// terminated together with the session. Mirrors matter.js's
// `packages/protocol/src/session/SessionManager.ts` close-callback
// chain — without the cascade, subscriptions linger and the engine
// keeps shipping reports to a peer whose decryption keys are gone,
// which from Apple's side looks like duplicate noise and disables
// the HMOutlet UI update.
//
// Passing nil clears any prior hook. The callback fires outside the
// manager's lock; it MUST be re-entrant-safe with respect to its own
// state but may call back into other manager APIs safely.
func (m *Manager) SetOnSessionClose(fn func(sessionID uint16)) {
	m.mu.Lock()
	m.onSessionClose = fn
	m.mu.Unlock()
}

// SetOnPaseSessionClose installs a hook that fires once per closed
// session that was established via PASE — after the [Manager.SetOnSessionClose]
// hook, with the same session id — whatever closed it: the peer's
// CloseSession, the fail-safe expiry and CommissioningComplete
// ([Manager.ClosePASESessions]), a revoked window, an eviction. A session
// adopted onto a fabric by [Manager.AdoptFabricIndex] stays a PASE
// session here. The node resumes its commissionable advertisement on it,
// as matter.js's DeviceAdvertiser does on `sessions.deleted` for a
// session with `isPase` (packages/protocol/src/protocol/DeviceAdvertiser.ts:131-162
// at 862ad75f). Passing nil clears any prior hook; the callback fires
// outside the manager's lock.
func (m *Manager) SetOnPaseSessionClose(fn func(sessionID uint16)) {
	m.mu.Lock()
	m.onPaseSessionClose = fn
	m.mu.Unlock()
}

// fireOnSessionClose dispatches the close hooks for every entry in
// entries. Caller MUST hold no manager locks. nil entries / nil hooks
// are no-ops.
func (m *Manager) fireOnSessionClose(entries []*Entry) {
	m.mu.RLock()
	hook, paseHook := m.onSessionClose, m.onPaseSessionClose
	m.mu.RUnlock()
	if hook == nil && paseHook == nil {
		return
	}
	for _, e := range entries {
		if e == nil {
			continue
		}
		if hook != nil {
			hook(e.SessionID)
		}
		if paseHook != nil && e.IsPASE() {
			paseHook(e.SessionID)
		}
	}
}

// ResumptionStore is the subset of [store.Store] the manager
// consumes. Defined as an interface so tests pass an in-memory fake.
type ResumptionStore interface {
	UpsertResumption(ctx context.Context, rec store.ResumptionRecord) error
	GetResumptionByID(ctx context.Context, resumptionID []byte) (store.ResumptionRecord, error)
	GetResumptionByPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (store.ResumptionRecord, error)
	RemoveResumption(ctx context.Context, fabricIndex uint8, peerNodeID uint64) error
}

// SessionIdleTimeout is the inactivity duration after which an
// operational session is eligible for eviction by the reaper.
// Mirrors matter.js packages/protocol/src/session/Session.ts
// SESSION_IDLE_INTERVAL_MS (default 5000 ms, but the session-
// eviction poll in go-fabric uses a more conservative 5 minutes
// to avoid churning short-lived idle windows between Apple Home
// subscription reports which arrive every 30–60 s).
//
// Per Matter §4.13.2.6.1 the spec defines SessionIdleInterval and
// SessionActiveInterval; our reaper uses a single conservative
// threshold that covers both subscriber-idle and full-session-idle
// scenarios without tracking the CASE vs. PASE state machine.
const SessionIdleTimeout = 5 * time.Minute

// PlaceholderIDTTL is how long a session id staked by
// [Manager.AllocateID] survives without the handshake that reserved it
// reaching [Manager.OpenFromSigmaWithID]. Past it the reaper hands the
// slot back.
//
// matter.js has no equivalent because it never reserves an id ahead of
// the session: `getNextAvailableSessionId`
// (packages/protocol/src/session/SessionManager.ts:537) is called at
// the moment the secure session is created. Our CASE responder must
// announce the id in Sigma2, one round-trip earlier, so an aborted
// handshake leaves an id behind and something has to reclaim it —
// otherwise a peer that opens exchanges and never answers Sigma2 walks
// the daemon through all 65534 slots and every later CASE handshake,
// legitimate ones included, is refused until a restart.
//
// The value must outlive every window in which the staked id can still
// be claimed, because reclaiming one early would hand a live handshake
// an id another session already owns. Two such windows exist: the
// bridge's per-exchange CASE adapter TTL (60 s — past it the handshake
// can never complete) and a commissioning window, whose PASE
// placeholder is staked when the window opens and claimed only when
// PASE completes, up to the spec maximum of 15 minutes later
// (Matter §5.4.2.3).
const PlaceholderIDTTL = 20 * time.Minute

// SessionIDSpace is the number of session ids the allocator can hand
// out: [1, 0xFFFE]. Id 0 is reserved by Matter for unsecured traffic and
// the allocator never issues 0xFFFF.
const SessionIDSpace = 0xFFFE

// Entry pairs a [channel.Session] with its operational metadata.
type Entry struct {
	// SessionID is the local 16-bit session identifier carried in
	// every encrypted Message Header.
	SessionID uint16
	// fabricIndex is the fabric this session belongs to. The bridge
	// uses it to scope Read / Write / Invoke against fabric-scoped
	// attributes (e.g. ACL). Guarded by mu — [Manager.AdoptFabricIndex]
	// rewrites it after the entry has already been handed out to
	// callers (e.g. a fabric-resolver closure captured via
	// [Manager.Get]), so an unguarded field would race against that
	// rewrite. Read via [Entry.FabricIndex].
	fabricIndex uint8
	// Session encrypts/decrypts messages for this peer.
	Session *channel.Session
	// AttestationChallenge is the 16-byte HKDF-derived challenge
	// chip-tool's commissioning flow signs over together with the
	// AttestationElements (Matter §11.18.4.7) and NOCSRElements
	// (§11.18.7.6). For PASE it's the third 16-byte slice of the
	// HKDF-SHA256(IKM=Ke, info="SessionKeys") output; for CASE it
	// comes from the Sigma key schedule.
	AttestationChallenge []byte
	// allocatedAt is when [Manager.AllocateID] staked this entry as a
	// placeholder for an in-flight handshake. It stays zero for
	// entries that were opened with a session directly. Guarded by the
	// manager's mu (not the entry's) — written once at allocation and
	// read by the reaper and the id allocator, both of which already
	// hold that lock.
	allocatedAt time.Time
	// lastActivity is updated on every inbound Decrypt and outbound
	// Encrypt call so the reaper can evict long-idle entries.
	// Mirrors chip src/transport/SecureSession.h:240-248 MarkActive /
	// MarkActiveRx.
	lastActivity time.Time
	// lastPeerActivity is set when the session is created and then
	// updated only on inbound decrypts — the peer-active determination
	// for MRP interval selection needs the time the PEER last sent, not
	// our own transmissions. Mirrors chip GetLastPeerActivityTime and
	// matter.js NodeSession.ts:121 at db021da0.
	lastPeerActivity time.Time
	// peerIdleIntervalMs / peerActiveIntervalMs / peerActiveThresholdMs
	// carry the peer's advertised MRP session parameters (Sigma1 /
	// PBKDFParamRequest tag 5). Zero means "not advertised" — readers
	// fall back to the spec defaults (matter.js SessionIntervals.ts:45-49).
	peerIdleIntervalMs    uint32
	peerActiveIntervalMs  uint32
	peerActiveThresholdMs uint32
	// peerTransports / peerMaxTCPMessageSize: the peer's effective
	// SupportedTransports bitmap and TCP message bound (0 = not stated).
	peerTransports        uint16
	peerMaxTCPMessageSize uint32
	// isPase marks a session established via the PASE handshake. The
	// marker survives [Manager.AdoptFabricIndex] (the session stays a
	// PASE session even after AddNOC rewrites its FabricIndex) so
	// [Manager.ClosePASESessions] can honour Matter §11.10.6.6 step 4.
	// Mirrors matter.js SessionManager.ts:484 getPaseSession, which
	// identifies the PASE session by type, not by fabric.
	isPase bool
	// isInitiator marks a session this node opened as the CASE
	// initiator. Such sessions never claim a slot of the peer's
	// per-peer budget (matter.js SessionManager.ts:650-652: "ones we
	// initiated are ours to manage").
	isInitiator bool
	// createdAt is when the session was installed with its keys. The
	// peer-loss sweep spares sessions created at or after the exchange
	// that reported the loss (matter.js SessionManager.ts:688-690).
	createdAt time.Time
	// lastActivityMono, lastPeerActivityMono and createdAtMono are the
	// same stamps on the clock's monotonic reading (clock.MonotonicOf): the
	// readings every interval is judged on — the peer-active threshold,
	// the per-peer LRU eviction — so a wall-clock step neither ages nor
	// rejuvenates a session. The wall times above are what the stamps
	// read as, for [SessionInfo]. matter.js keeps its timestamps on
	// Time.nowUs, the monotonic reading (Session.ts:141-151 at 862ad75f),
	// and exposes them through the wall-clock offset (:154-162).
	lastActivityMono, lastPeerActivityMono, createdAtMono time.Duration
	// clock is the manager's clock when the entry opened; nil (an entry
	// built outside a manager) is clock.System.
	clock clock.Clock
	// mu guards the mutable fields above for concurrent Rx/Tx updates.
	mu sync.Mutex
}

// MarkActiveRx updates the entry's activity timestamp after a
// successful inbound message decryption.
func (e *Entry) MarkActiveRx() {
	now, mono := clock.Or(e.clock).Now(), clock.MonotonicOf(e.clock)
	e.mu.Lock()
	e.lastActivity, e.lastActivityMono = now, mono
	e.lastPeerActivity, e.lastPeerActivityMono = now, mono
	e.mu.Unlock()
}

// SetPeerMRPIntervals stores the peer-advertised MRP session
// parameters (milliseconds; 0 = not advertised, readers fall back to
// the spec default for that field). Called once at session open with
// the values the initiator carried in Sigma1 / PBKDFParamRequest tag 5.
func (e *Entry) SetPeerMRPIntervals(idleMs, activeMs, activeThresholdMs uint32) {
	e.mu.Lock()
	e.peerIdleIntervalMs = idleMs
	e.peerActiveIntervalMs = activeMs
	e.peerActiveThresholdMs = activeThresholdMs
	e.mu.Unlock()
}

// PeerMRPIntervals returns what the peer advertised in its handshake's
// SessionParameters (milliseconds; 0 = not advertised) — the device's
// record of the controller's intervals, the counterpart of
// client/exchange.Session.ReportedParameters on the initiator (matter.js
// NodeSession.ts:136, :175 reportedParameters, ReportedSessionParametersTest.ts).
func (e *Entry) PeerMRPIntervals() (idleMs, activeMs, activeThresholdMs uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peerIdleIntervalMs, e.peerActiveIntervalMs, e.peerActiveThresholdMs
}

// SetPeerTransportParameters stores what the peer stated about transports
// in its SessionParameters: the SupportedTransports bitmap and
// MaxTCPMessageSize, already passed through the Matter 1.5 gate
// ([sessionparams.Parameters.EffectiveTransports]). A TCP sender bounds
// its messages to the peer's size, as matter.js keeps
// `maxTCPMessageSize` on the Session (Session.ts:86, :118).
func (e *Entry) SetPeerTransportParameters(transports uint16, maxTCPMessageSize uint32) {
	e.mu.Lock()
	e.peerTransports = transports
	e.peerMaxTCPMessageSize = maxTCPMessageSize
	e.mu.Unlock()
}

// PeerTransportParameters returns the bitmap and the TCP message bound
// stored by [Entry.SetPeerTransportParameters]; zero when the peer stated
// none.
func (e *Entry) PeerTransportParameters() (transports uint16, maxTCPMessageSize uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peerTransports, e.peerMaxTCPMessageSize
}

// RetransmitBaseInterval returns the MRP base interval for the next
// (re)transmission to this peer: the peer's active interval when it
// sent a message within its active threshold, its idle interval
// otherwise. Fields the peer never advertised fall back to the spec
// defaults. Mirrors matter.js MRP.ts:129-135 retransmissionIntervalOf
// ("baseInterval = isPeerActive ? activeInterval : idleInterval",
// re-evaluated per transmission) and chip GetMRPBaseTimeout.
func (e *Entry) RetransmitBaseInterval(_ time.Time) time.Duration {
	// The peer-active decision is an interval, judged on the clock's
	// monotonic reading so a wall-clock step cannot flip it (matter.js
	// Session.ts:149-151 on Time.nowUs); the caller's wall reading is
	// accepted for the signature's sake and not consulted.
	e.mu.Lock()
	lastPeer, lastPeerMono := e.lastPeerActivity, e.lastPeerActivityMono
	idleMs, activeMs, thresholdMs := e.peerIdleIntervalMs, e.peerActiveIntervalMs, e.peerActiveThresholdMs
	e.mu.Unlock()
	nowMono := clock.MonotonicOf(e.clock)

	idle := mrp.SessionIdleIntervalDefault
	if idleMs != 0 {
		idle = time.Duration(idleMs) * time.Millisecond
	}
	active := mrp.SessionActiveIntervalDefault
	if activeMs != 0 {
		active = time.Duration(activeMs) * time.Millisecond
	}
	threshold := mrp.SessionActiveThresholdDefault
	if thresholdMs != 0 {
		threshold = time.Duration(thresholdMs) * time.Millisecond
	}
	if !lastPeer.IsZero() && nowMono-lastPeerMono < threshold {
		return active
	}
	return idle
}

// MarkActiveTx updates the entry's activity timestamp after a
// successful outbound message encryption.
func (e *Entry) MarkActiveTx() {
	now, mono := clock.Or(e.clock).Now(), clock.MonotonicOf(e.clock)
	e.mu.Lock()
	e.lastActivity, e.lastActivityMono = now, mono
	e.mu.Unlock()
}

// lastActivityMonotonic is the monotonic reading of the last traffic, 0
// for an entry that never carried any.
func (e *Entry) lastActivityMonotonic() (time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastActivityMono, !e.lastActivity.IsZero()
}

// LastActivity returns the last time this session exchanged a message.
// Returns the zero time for entries that have never been used (e.g.
// pre-allocated placeholders).
func (e *Entry) LastActivity() time.Time {
	e.mu.Lock()
	t := e.lastActivity
	e.mu.Unlock()
	return t
}

// CreatedAt returns when the session was installed with its keys; the
// zero time for a pre-allocated placeholder.
func (e *Entry) CreatedAt() time.Time {
	e.mu.Lock()
	t := e.createdAt
	e.mu.Unlock()
	return t
}

// CreatedAtMonotonic is the clock's monotonic reading ([clock.MonotonicOf])
// when the session was installed with its keys — what a peer-loss cutoff
// compares against ([Manager.ClosePeerLostSince]); 0 for a placeholder.
func (e *Entry) CreatedAtMonotonic() time.Duration {
	e.mu.Lock()
	t := e.createdAtMono
	e.mu.Unlock()
	return t
}

// FabricIndex returns the fabric this session currently belongs to.
// Safe to call concurrently with [Manager.AdoptFabricIndex] — callers
// that resolve a fabric off an [Entry] captured earlier (e.g. via
// [Manager.Get]) MUST go through this accessor rather than reading a
// struct field, since AdoptFabricIndex can rewrite the value after the
// Entry pointer has already been handed out.
func (e *Entry) FabricIndex() uint8 {
	e.mu.Lock()
	v := e.fabricIndex
	e.mu.Unlock()
	return v
}

// IsPASE reports whether this session was established via the PASE
// handshake. The marker is independent of [Entry.FabricIndex]: after
// AddNOC, [Manager.AdoptFabricIndex] rewrites the fabric while the
// session stays a PASE session, so the two answers deliberately
// disagree for the commissioning channel. The bridge reads this to
// stamp [im.WithAuthModePASE] onto inbound requests, which keeps the
// implicit Administer grant matter.js keys on the auth mode
// (packages/protocol/src/interaction/FabricAccessControl.ts:189-191).
func (e *Entry) IsPASE() bool {
	e.mu.Lock()
	v := e.isPase
	e.mu.Unlock()
	return v
}

// setFabricIndex rewrites the fabric this session belongs to. Only
// [Manager.AdoptFabricIndex] calls this — kept unexported so the
// rewrite always goes through the manager's documented contract.
func (e *Entry) setFabricIndex(v uint8) {
	e.mu.Lock()
	e.fabricIndex = v
	e.mu.Unlock()
}

// Errors.
var (
	// ErrSessionExhausted is returned when [1, 0xFFFE] is fully
	// occupied — pathological case, the bridge would never reach it
	// in practice.
	ErrSessionExhausted = errors.New("operational: session id space exhausted")
	// ErrSessionNotFound is returned by [Manager.Get] / [Manager.Close]
	// when no session matches the supplied id.
	ErrSessionNotFound = errors.New("operational: session not found")
)

// SetClock sets the clock the manager stamps sessions with and paces the
// idle reaper on; nil is [clock.System]. Call it before the first session
// opens: an entry keeps the clock it was opened under.
func (m *Manager) SetClock(c clock.Clock) { m.clock.Store(c) }

// clk is the manager's clock.
func (m *Manager) clk() clock.Clock { return m.clock.Load() }

// NewManager returns a manager backed by s.
func NewManager(s ResumptionStore) *Manager {
	return &Manager{
		store:    s,
		sessions: make(map[uint16]*Entry),
		nextID:   randomInitialSessionID(),
	}
}

// randomInitialSessionID picks a random starting point in [1, 0xFFFE]
// for the session-id allocator. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:213
// (`this.#nextSessionId = crypto.randomUint16`). Starting at a random
// slot rather than always at 1 reduces the chance that, after a daemon
// restart, the bridge re-issues a low session id that a peer still has
// cached from a prior session — which would otherwise let the peer's
// new traffic land on a session-table slot it misidentifies. Falls
// back to 1 if the entropy source is unavailable.
func randomInitialSessionID() uint16 {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	v := uint16(b[0])<<8 | uint16(b[1])
	// Map onto the allocator's valid range [1, 0xFFFE]; id 0 is
	// reserved by Matter for unsecured traffic and 0xFFFF is out of
	// range for allocateIDLocked.
	return v%0xFFFE + 1
}

// OpenFromSigma constructs a session from the Sigma key-derivation
// output and registers it under a freshly allocated session id.
//
// localNodeID is the bridge's NodeID inside fabricIndex; peerNodeID
// is the commissioner's node identifier extracted from the verified
// peer NOC.
//
// A new CASE does not close the peer's previous sessions: the peer
// keeps every session it established until it holds more than
// [MaxSessionsPerPeer], at which point the least recently active ones
// are force-closed (no CloseSession on the wire). Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:185-189
// (MAX_SESSIONS_PER_PEER) and :641-680 (#evictExcessSessionsFor).
func (m *Manager) OpenFromSigma(fabricIndex uint8, localNodeID, peerNodeID uint64, keys sigma.SessionKeys) (*Entry, error) {
	sess, err := channel.New(channel.Config{
		EncryptKey:  keys.R2IKey[:], // bridge → peer
		DecryptKey:  keys.I2RKey[:], // peer → bridge
		LocalNodeID: localNodeID,
		PeerNodeID:  peerNodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("operational: build session: %w", err)
	}

	m.mu.Lock()
	id, err := m.allocateIDLocked()
	if err != nil {
		m.mu.Unlock()
		sess.Close()
		return nil, err
	}
	entry := &Entry{
		SessionID:   id,
		fabricIndex: fabricIndex,
		Session:     sess,
		// As openFromSigma: the Sigma key schedule's third block
		// (matter.js NodeSession.ts:82).
		AttestationChallenge: append([]byte(nil), keys.AttestationChallenge[:]...),
		createdAt:            m.clk().Now(),
		// The peer counts active from the session's creation: matter.js
		// NodeSession.ts:121 at db021da0 ("We always set the active timestamp for Secure
		// sessions"), chip SecureSession.h:349 (mLastPeerActivityTime = now).
		lastPeerActivity:     m.clk().Now(),
		lastPeerActivityMono: clock.MonotonicOf(m.clk()),
		createdAtMono:        clock.MonotonicOf(m.clk()),
		clock:                m.clk(),
	}
	m.sessions[id] = entry
	excess := m.evictExcessPeerSessionsLocked(entry)
	m.mu.Unlock()
	// Force close, no CloseSession: matter.js SessionManager.ts:676-678
	// skips the graceful path for the cap because the eviction target
	// is typically the peer it can no longer reach.
	closeStaleEntries(excess)
	m.fireOnSessionClose(excess)
	return entry, nil
}

// AllocateID reserves the next-available session id without
// constructing a Session under it. The caller is responsible for
// passing the same id back to [Manager.OpenFromSigmaWithID] before
// the slot is reaped — and for releasing the slot via [Manager.ReleaseID]
// if the handshake aborts before reaching key derivation. A handshake
// that does neither loses the slot to the reaper after
// [PlaceholderIDTTL].
//
// CASE responders need to know their allocated id BEFORE Sigma2 is
// built, because Sigma2.responderSessionID is what the commissioner
// echoes back as `dest.SessionID` on every operational packet.
// Hard-coding "1" (the previous behaviour) would collide every
// parallel CASE session into a single lookup slot and break peer →
// bridge routing.
func (m *Manager) AllocateID() (uint16, error) {
	m.mu.Lock()
	id, err := m.allocateIDLocked()
	// Every id held by a live session — one fabric's administrator can
	// open them for as many node ids as it issues NOCs: the least recently
	// active session is closed and its id reused, as matter.js closes
	// findOldestInactiveSession's session and takes its id
	// (packages/protocol/src/session/SessionManager.ts:590-598, :564-572
	// at db021da0). Its close runs below, outside the lock (review II,
	// R2-P3-admin-05).
	var evicted *Entry
	if errors.Is(err, ErrSessionExhausted) {
		if evicted = m.leastRecentlyActiveLocked(); evicted != nil {
			delete(m.sessions, evicted.SessionID)
			id, err = evicted.SessionID, nil
		}
	}
	if err != nil {
		m.mu.Unlock()
		return 0, err
	}
	defer m.closeEvicted(evicted)
	defer m.mu.Unlock()
	// Stake a placeholder so concurrent allocators do not hand the
	// same id out twice; OpenFromSigmaWithID overwrites it. The stamp
	// is what lets the reaper tell an in-flight handshake from one
	// that was abandoned — see [PlaceholderIDTTL].
	e := &Entry{SessionID: id, allocatedAt: m.clk().Now(), clock: m.clk()}
	m.sessions[id] = e
	m.placeholders = append(m.placeholders, placeholderRef{id: id, e: e})
	m.compactPlaceholdersLocked()
	return id, nil
}

// leastRecentlyActiveLocked returns the live session with the least recent
// traffic, any fabric, PASE or CASE, as matter.js's findOldestInactiveSession
// filters none (SessionManager.ts:564-572, #leastRecentlyUsedSession
// :553-562 at db021da0); nil when no live session exists. The order is
// [lessRecentlyActive]'s. Caller holds m.mu.
func (m *Manager) leastRecentlyActiveLocked() *Entry {
	var oldest *Entry
	for _, e := range m.sessions {
		if e.Session == nil {
			continue
		}
		if oldest == nil || lessRecentlyActive(e, oldest) {
			oldest = e
		}
	}
	return oldest
}

// closeEvicted closes the session [Manager.AllocateID] took the id of,
// outside the manager's lock, as every local close does: the peer is told
// with a CloseSession (matter.js initiateClose on the evicted session,
// SessionManager.ts:594-596; "A final exchange will be opened to notify the
// peer of closure unless the peer is marked as lost", NodeSession.ts:365 at
// db021da0), then the keys go and the hooks fire. Nil is nothing to close.
func (m *Manager) closeEvicted(e *Entry) {
	if e == nil {
		return
	}
	victims := []*Entry{e}
	m.notifyGracefulClose(victims, time.Time{})
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	m.fireReannounceIfPeerGone(victims)
}

// placeholderRef is one id [Manager.AllocateID] staked and the entry it
// staked it with.
type placeholderRef struct {
	id uint16
	e  *Entry
}

// stakes reports whether ref's id still holds the placeholder ref staked.
// Caller holds m.mu.
func (m *Manager) stakes(ref placeholderRef) bool {
	cur, ok := m.sessions[ref.id]
	return ok && cur == ref.e && cur.Session == nil
}

// compactPlaceholdersLocked drops the queue's references to ids that no
// longer hold their placeholder once they outnumber the table twice over,
// so the queue stays within a constant factor of the table and each
// reference is dropped once (amortised constant time per allocation).
// Caller holds m.mu in write mode.
func (m *Manager) compactPlaceholdersLocked() {
	if len(m.placeholders) <= 2*len(m.sessions)+placeholderQueueSlack {
		return
	}
	m.placeholders = slices.DeleteFunc(m.placeholders, func(ref placeholderRef) bool { return !m.stakes(ref) })
}

// placeholderQueueSlack keeps a small table from compacting its placeholder
// queue on every allocation; any constant does, the bound is the factor
// two in [Manager.compactPlaceholdersLocked].
const placeholderQueueSlack = 64

// ReleaseID frees a previously allocated id when the handshake that
// reserved it never reached OpenFromSigmaWithID.
func (m *Manager) ReleaseID(id uint16) {
	m.mu.Lock()
	if e, ok := m.sessions[id]; ok && e.Session == nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

// OpenFromSigmaWithID is a variant of [Manager.OpenFromSigma] that
// registers the new session under a pre-allocated id (see
// [Manager.AllocateID]). It overwrites any placeholder entry left
// by AllocateID.
//
// peerSessionID is the InitiatorSessionID the commissioner sent in
// Sigma1 — every outbound encrypted reply must carry this in
// Header.SessionID so the peer can resolve the session in its own
// table. Without it Apple Home / chip-tool drops every reply.
//
// peerCATs is the set of CASE Authenticated Tags lifted out of the
// peer's NOC subject at Sigma3 verification. Stored on the session
// so the IM dispatcher's ACL gate can match CAT-bearing ACEs (Matter
// §9.10.5.6). Pass nil when the peer NOC carried no CATs.
func (m *Manager) OpenFromSigmaWithID(id uint16, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(id, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, false)
}

// OpenFromSigmaAsInitiatorWithID is [Manager.OpenFromSigmaWithID] for a
// session this node opened as the CASE initiator: it encrypts with the
// I2R key and decrypts with R2I. The only initiator in this module
// re-establishes former subscriptions after a restart (docs/adr/0008);
// matter.js creates the same session from CaseClient.ts:#doPair with
// `isInitiator: true`.
func (m *Manager) OpenFromSigmaAsInitiatorWithID(id uint16, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(id, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, true)
}

func (m *Manager) openFromSigma(id uint16, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys, initiator bool) (*Entry, error) {
	encryptKey, decryptKey := keys.R2IKey[:], keys.I2RKey[:]
	if initiator {
		encryptKey, decryptKey = keys.I2RKey[:], keys.R2IKey[:]
	}
	sess, err := channel.New(channel.Config{
		EncryptKey:    encryptKey,
		DecryptKey:    decryptKey,
		LocalNodeID:   localNodeID,
		PeerNodeID:    peerNodeID,
		PeerCATs:      peerCATs,
		PeerSessionID: peerSessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("operational: build session: %w", err)
	}
	// AttestationChallenge is the third 16-byte slice of the 48-byte
	// HKDF-SHA256(IKM=sharedSecret, salt=sessionKeysSalt, info="SessionKeys")
	// output produced by the Sigma key schedule.
	// Mirrors matter.js packages/protocol/src/session/NodeSession.ts:82 —
	// `const attestationKey = keys.slice(32, 48)` — and
	// chip CASESession.cpp:615 — derived from the same 48-byte block.
	// OpenFromPase sets this identically (manager.go:241); CASE must match.
	entry := &Entry{
		SessionID:            id,
		fabricIndex:          fabricIndex,
		Session:              sess,
		AttestationChallenge: append([]byte(nil), keys.AttestationChallenge[:]...),
		isInitiator:          initiator,
		createdAt:            m.clk().Now(),
		// The peer counts active from the session's creation: matter.js
		// NodeSession.ts:121 at db021da0 ("We always set the active timestamp for Secure
		// sessions"), chip SecureSession.h:349 (mLastPeerActivityTime = now).
		lastPeerActivity:     m.clk().Now(),
		lastPeerActivityMono: clock.MonotonicOf(m.clk()),
		createdAtMono:        clock.MonotonicOf(m.clk()),
		clock:                m.clk(),
	}
	m.mu.Lock()
	// The pre-allocated id is in m.sessions as a placeholder Entry
	// (Session==nil) that is overwritten here. A LIVE session can hold
	// the slot too; it goes through the graceful close cascade rather
	// than being dropped silently. Tell its peer BEFORE zeroising the
	// keys — matter.js emits gracefulClose (NodeSession.ts:403) while
	// the session can still encrypt, and the exchange layer ships the
	// CloseSession StatusReport (ExchangeManager.ts:716-726).
	var occupied []*Entry
	if occupant := m.installEntryLocked(id, entry); occupant != nil {
		occupied = append(occupied, occupant)
	}
	excess := m.evictExcessPeerSessionsLocked(entry)
	m.mu.Unlock()
	m.notifyGracefulClose(occupied, time.Time{})
	closeStaleEntries(occupied)
	m.fireOnSessionClose(occupied)
	// The per-peer cap force-closes without CloseSession — see
	// OpenFromSigma for the matter.js provenance.
	closeStaleEntries(excess)
	m.fireOnSessionClose(excess)
	return entry, nil
}

// OpenFromPase constructs a PASE session from the Spake2+ shared
// secret (Ke) and registers it under a freshly allocated session id.
// PASE keys are derived via HKDF-SHA256(IKM=Ke, salt="", info="SessionKeys", L=48)
// per Matter §4.13.4.2 — the resulting 48 bytes split into
// I2RKey (16) || R2IKey (16) || AttestationChallenge (16).
//
// PASE sessions never carry a fabric (commissioning happens before
// the operational fabric exists). The Entry's FabricIndex stays 0
// to signal that; callers that need to gate fabric-scoped operations
// must check for FabricIndex==0.
func (m *Manager) OpenFromPase(localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*Entry, error) {
	sess, challenge, err := paseSession(localNodeID, peerNodeID, peerSessionID, sharedSecret)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	id, err := m.allocateIDLocked()
	if err != nil {
		m.mu.Unlock()
		sess.Close()
		return nil, err
	}
	entry := &Entry{
		SessionID:            id,
		fabricIndex:          0, // PASE pre-dates the operational fabric
		Session:              sess,
		AttestationChallenge: challenge,
		isPase:               true,
		createdAt:            m.clk().Now(),
		// The peer counts active from the session's creation: matter.js
		// NodeSession.ts:121 at db021da0 ("We always set the active timestamp for Secure
		// sessions"), chip SecureSession.h:349 (mLastPeerActivityTime = now).
		lastPeerActivity:     m.clk().Now(),
		lastPeerActivityMono: clock.MonotonicOf(m.clk()),
		createdAtMono:        clock.MonotonicOf(m.clk()),
		clock:                m.clk(),
	}
	m.sessions[id] = entry
	m.mu.Unlock()
	return entry, nil
}

// paseSession runs the PASE key schedule of [Manager.OpenFromPase] and
// returns the session built on the two keys and a copy of the attestation
// challenge.
//
// The 48 derived bytes have served once the session holds its own copies
// of the keys (channel.New copies both; TestNewKeepsItsOwnKeys in
// secure/channel) and the challenge is copied out: they are overwritten on
// every way out, as the initiator overwrites its own
// (client/handshake/pase.go) and connectedhomeip the secret they come from
// (src/crypto/CHIPCryptoPALOpenSSL.cpp:1150-1151 at 6170af84,
// `ClearSecretData(Kae)`). Best effort, within Go's limits
// (secure/sigma/zero.go): crypto/hkdf and crypto/aes keep working state of
// their own. sharedSecret is the caller's and is not touched; what lives
// on is what the session is — its two keys, for as long as it is open, and
// the challenge in the entry (review II, W1-sec-05, R2-P0-rv-B03).
func paseSession(localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*channel.Session, []byte, error) {
	if len(sharedSecret) == 0 {
		return nil, nil, errors.New("operational: empty PASE shared secret")
	}
	derived, err := hkdf.Key(sha256.New, sharedSecret, nil, "SessionKeys", 48)
	if err != nil {
		return nil, nil, fmt.Errorf("operational: PASE hkdf: %w", err)
	}
	defer func() {
		secretHook("pase.sessionKeyMaterial", derived)
		clear(derived)
	}()
	sess, err := channel.New(channel.Config{
		EncryptKey:    derived[16:32], // R2IKey — bridge → peer (responder→initiator)
		DecryptKey:    derived[0:16],  // I2RKey — peer → bridge
		LocalNodeID:   localNodeID,
		PeerNodeID:    peerNodeID,
		PeerSessionID: peerSessionID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("operational: build PASE session: %w", err)
	}
	return sess, append([]byte(nil), derived[32:48]...), nil
}

// secretHook is the test seam that sees a short-lived secret buffer just
// before it is overwritten, so a test can hold the slice and read it
// afterwards. Production code never replaces it.
var secretHook = func(string, []byte) {}

// OpenFromPaseWithID is a variant of [Manager.OpenFromPase] that
// registers the new PASE session under a pre-allocated id (see
// [Manager.AllocateID]). It overwrites any placeholder entry left by
// AllocateID.
//
// Use this when the responder session id must be known BEFORE the
// PBKDFParamResponse is sent — the commissioner echoes it back as
// Header.SessionID on every post-PASE-establishment packet (IM reads,
// writes, invokes). Pre-allocating the id and embedding it in the
// PBKDFParamResponse guarantees the bridge's session lookup resolves
// the correct entry for every subsequent datagram, even when other
// sessions occupy the lower-numbered slots.
func (m *Manager) OpenFromPaseWithID(id uint16, localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*Entry, error) {
	sess, challenge, err := paseSession(localNodeID, peerNodeID, peerSessionID, sharedSecret)
	if err != nil {
		return nil, err
	}
	entry := &Entry{
		SessionID:            id,
		fabricIndex:          0, // PASE pre-dates the operational fabric
		Session:              sess,
		AttestationChallenge: challenge,
		isPase:               true,
		createdAt:            m.clk().Now(),
		// The peer counts active from the session's creation: matter.js
		// NodeSession.ts:121 at db021da0 ("We always set the active timestamp for Secure
		// sessions"), chip SecureSession.h:349 (mLastPeerActivityTime = now).
		lastPeerActivity:     m.clk().Now(),
		lastPeerActivityMono: clock.MonotonicOf(m.clk()),
		createdAtMono:        clock.MonotonicOf(m.clk()),
		clock:                m.clk(),
	}
	m.mu.Lock()
	displaced := m.installEntryLocked(id, entry)
	m.mu.Unlock()
	if displaced != nil {
		m.notifyGracefulClose([]*Entry{displaced}, time.Time{})
		closeStaleEntries([]*Entry{displaced})
		m.fireOnSessionClose([]*Entry{displaced})
	}
	return entry, nil
}

// installEntryLocked registers entry under id and returns the LIVE
// session it displaced, if any, for the caller to run through the
// close cascade outside the lock. Returns nil when the slot was free
// or held only the placeholder [Manager.AllocateID] staked.
//
// A live occupant is never simply overwritten: dropping it from the
// table leaves its key material live and its subscriptions registered
// against an id that now belongs to somebody else, so the report pump
// would keep serving the old peer's subscriptions under the new peer's
// keys. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:490-508, where
// getNextAvailableSessionId closes a session before reusing its id.
//
// The caller must hold m.mu in write mode.
func (m *Manager) installEntryLocked(id uint16, entry *Entry) *Entry {
	displaced := m.sessions[id]
	m.sessions[id] = entry
	if displaced == nil || displaced.Session == nil {
		return nil
	}
	return displaced
}

// Get looks up an established session by id.
//
// An id that [Manager.AllocateID] has merely reserved — the CASE/PASE
// handshake that claimed it has not reached key derivation, or aborted
// before it — is reported as [ErrSessionNotFound]. The reservation is a
// placeholder with no Session under it, and every caller of Get uses the
// entry to reach that Session: handing the stub out reads to them as "a
// usable session exists", so a peer that echoes its pre-allocated
// responderSessionID back in an ordinary secure datagram makes the
// receive path dereference a nil session and panic. The panic is
// swallowed by the listener's per-datagram recover, so the only symptom
// is a controller whose messages vanish.
//
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:513
// getSession, whose map holds only constructed sessions — it reserves an
// id by asserting getSession(id) is undefined rather than by staking a
// stub, so a reserved id resolves to nothing there. The stub is ours
// alone (it keeps two concurrent allocators apart); keeping it out of
// Get restores the same observable contract.
func (m *Manager) Get(sessionID uint16) (*Entry, error) {
	m.mu.RLock()
	entry, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok || entry == nil || entry.Session == nil {
		return nil, ErrSessionNotFound
	}
	return entry, nil
}

// AttestationChallengeFor returns the attestation challenge of the session
// sessionID (Matter §4.13.2.6), or false when the session is unknown or has
// none. SignVIDVerificationRequest signs over the challenge of the session
// it arrived on (matter.js session.attestationChallengeKey).
func (m *Manager) AttestationChallengeFor(sessionID uint16) ([]byte, bool) {
	m.mu.RLock()
	entry, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok || entry == nil || len(entry.AttestationChallenge) == 0 {
		return nil, false
	}
	return append([]byte(nil), entry.AttestationChallenge...), true
}

// AdoptFabricIndex rewrites the FabricIndex on the session identified
// by sessionID. Used by the OperationalCredentials cluster's
// OnFabricInstalled hook: after a successful AddNOC the PASE session
// the commissioner used to send the command must "become" the new
// fabric so the subsequent IM commands (CommissioningComplete, ACL
// reads, group-key writes) find an accessing fabric.
//
// Mirrors chip's `SecureSession::AdoptFabricIndex(newFabricIndex)`
// invoked at OperationalCredentialsCluster.cpp:510-514 BEFORE the
// ACL replace. Without the adoption Apple's commissioner sends
// CommissioningComplete on the same session, the bridge sees
// FabricIndex=0 (PASE) for an access check that requires the just-
// installed fabric, and the operation fails with InvalidAuthentication
// — Apple aborts the pair attempt with `MTRErrorDomain Code=9
// "System Commissioner Pairing — Completing"`.
//
// Returns [ErrSessionNotFound] when no session matches sessionID.
// Idempotent: setting the same FabricIndex twice is a no-op.
func (m *Manager) AdoptFabricIndex(sessionID uint16, newFabricIndex uint8) error {
	m.mu.RLock()
	entry, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	entry.setFabricIndex(newFabricIndex)
	return nil
}

// Close terminates a session and frees its id slot.
//
// Used for peer-initiated teardown (an inbound Secure-Channel
// CloseSession StatusReport) among others, so it deliberately does
// NOT fire the graceful-close notifier — echoing a CloseSession back
// at a peer that just closed would be wrong. Mirrors matter.js
// NodeSession.ts:305-309 handlePeerClose, which marks the peer lost so
// close() skips the gracefulClose emit. The reannounce trigger still
// fires when the peer has no remaining session, matching matter.js
// DeviceAdvertiser.ts:132-149 (every session deletion conditionally
// resumes broadcast).
func (m *Manager) Close(sessionID uint16) error {
	m.mu.Lock()
	entry, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	// A pre-allocated id whose handshake never reached key derivation has
	// no Session under it; dropping the reservation is the whole teardown.
	if entry.Session != nil {
		entry.Session.Close()
	}
	m.fireOnSessionClose([]*Entry{entry})
	m.fireReannounceIfPeerGone([]*Entry{entry})
	return nil
}

// CloseAllGraceful tears down every live session, notifying each peer
// via the graceful-close hook (best-effort CloseSession StatusReport)
// before its keys are zeroised. Pre-allocated id placeholders are
// dropped silently. Returns the number of real sessions closed.
//
// deadline bounds the per-peer notification sweep: once passed, the
// remaining peers are closed locally without a wire notification so a
// daemon shutdown never blocks on the network. The reannounce trigger
// deliberately does not fire — the caller is shutting the bridge down
// and matter.js likewise kicks off the advertiser shutdown before removing
// sessions "to prevent re-announces when removing sessions" (at db021da0,
// packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:450-453).
//
// The CloseSession per session departs from matter.js at db021da0, whose
// shutdown flushes each session's subscriptions and then force-closes the
// session without a CloseSession (SessionManager.ts:1097-1113
// closeAllSessions: closeSubscriptionsForShutdown, then
// initiateForceClose, which marks the peer lost, NodeSession.ts:379-382,
// so close() skips gracefulClose, :414-425). The flush is mirrored: the
// bridge's Stop reports what the subscriptions owe before it calls this.
// The CloseSession is kept by decision (review II, owner decision 49;
// notes/parity/by_design.md, BD-Matter-Shutdown-CloseSessionKept).
func (m *Manager) CloseAllGraceful(deadline time.Time) int {
	m.mu.Lock()
	victims := make([]*Entry, 0, len(m.sessions))
	for id, e := range m.sessions {
		delete(m.sessions, id)
		if e == nil || e.Session == nil {
			continue // placeholder — no keys, no peer to notify
		}
		victims = append(victims, e)
	}
	m.mu.Unlock()
	m.notifyGracefulClose(victims, deadline)
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	return len(victims)
}

// CloseFabric tears down every session associated with fabricIndex.
// Used when a fabric is removed via OperationalCredentials.RemoveFabric.
func (m *Manager) CloseFabric(fabricIndex uint8) {
	m.mu.Lock()
	victims := make([]*Entry, 0)
	for id, entry := range m.sessions {
		if entry.FabricIndex() == fabricIndex {
			victims = append(victims, entry)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
}

// CloseFabricExcept tears down every session on fabricIndex except the one
// whose local session ID equals exceptSessionID. UpdateNOC must abort all
// OTHER CASE sessions on the rotated fabric while preserving the invoking
// session, so the NOCResponse still reaches the wire and the commissioner can
// re-CASE on that same session. Closing the invoking session here would drop
// its own response (the reply lookup fails on the deleted session) and the
// commissioner would time out. Mirrors chip
// FabricTable::AbortAllOtherCommunicationOnFabric, which pins the invoking
// exchange's session. exceptSessionID==0 falls back to closing every session
// on the fabric — no operational session is keyed under ID 0.
func (m *Manager) CloseFabricExcept(fabricIndex uint8, exceptSessionID uint16) {
	m.mu.Lock()
	victims := make([]*Entry, 0)
	for id, entry := range m.sessions {
		if entry.FabricIndex() == fabricIndex && id != exceptSessionID {
			victims = append(victims, entry)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
}

// ClosePASESessions tears down every session established via PASE —
// including sessions whose FabricIndex was later rewritten by
// [Manager.AdoptFabricIndex] (adoption does not change the session's
// PASE nature). Matter §11.10.6.6 step 4 requires the server to clear
// any still-established PASE session on a successful
// CommissioningComplete, and the fail-safe expiry path does the same.
// Mirrors matter.js FailsafeContext.ts:134 (completeCommission) and
// :280 (fail-safe expired) → closePaseSession. Returns the number of
// sessions closed.
func (m *Manager) ClosePASESessions() int {
	m.mu.Lock()
	victims := make([]*Entry, 0, 1)
	for id, entry := range m.sessions {
		if entry.isPase {
			victims = append(victims, entry)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	return len(victims)
}

// PASESessionIDs returns the ids of the established PASE sessions in the
// table, the ones AddNOC adopted onto a fabric included. The bridge accepts
// no new PASE exchange while one of them is not closing, as matter.js
// ignores a new PASE exchange while `getPaseSession()` finds a PASE session
// that is not closing (packages/protocol/src/session/SessionManager.ts:614-618,
// packages/protocol/src/session/pase/PaseServer.ts:80-82 at db021da0).
func (m *Manager) PASESessionIDs() []uint16 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []uint16
	for id, entry := range m.sessions {
		if entry.isPase && entry.Session != nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// ClosePeer tears down every live session matching the
// (fabricIndex, peerNodeID) pair, telling each peer with a CloseSession
// first. Returns the number of sessions closed. A peer that stopped
// answering is closed with [Manager.ClosePeerLost] instead.
//
// Deprecated: nothing in the module calls it — the KeepSubscriptions=false
// teardown it was written for closes the peer's subscriptions, not its
// sessions (subscription.Manager.ClosePeer). There is no replacement
// that sends the farewell; [Manager.Close] on each id
// [Manager.SessionIDsForPeer] returns closes the sessions without one.
// Removable in v0.23.0 (review II, R2-P2 design note 4).
func (m *Manager) ClosePeer(fabricIndex uint8, peerNodeID uint64) int {
	m.mu.Lock()
	victims := make([]*Entry, 0)
	for id, entry := range m.sessions {
		if entry.Session == nil {
			// Pre-allocated id placeholder — the handshake that owns
			// it has not reached OpenFromSigmaWithID yet. Leaving the
			// placeholder in place is harmless: the placeholder has
			// no key material, so an inbound packet for it cannot
			// authenticate.
			continue
		}
		if entry.FabricIndex() == fabricIndex && entry.Session.PeerNodeID() == peerNodeID {
			victims = append(victims, entry)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	// Graceful close notification before key zeroise, then the
	// conditional broadcast resume — matter.js ExchangeManager.ts:716-726
	// / DeviceAdvertiser.ts:132-149 (see the hook field docs).
	m.notifyGracefulClose(victims, time.Time{})
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	m.fireReannounceIfPeerGone(victims)
	return len(victims)
}

// ClosePeerLost force-closes every live session with the
// (fabricIndex, peerNodeID) pair that was created before asOf — the
// sessions a peer held when an exchange on one of them ran out of MRP
// retransmissions. No CloseSession goes on the wire: the peer is
// lost, so matter.js marks the session peer-lost and force-closes it
// (NodeSession.ts:325-328 handlePeerLoss → initiateForceClose), and
// the session manager applies that to every session of the peer
// created before the reporting exchange (SessionManager.ts:677-695
// #handlePeerLoss, the `asOf` check at :688). A zero asOf closes them
// all. Returns the number of sessions closed.
//
// asOf is compared with the session's wall-clock creation time: on
// [clock.System] both carry Go's monotonic reading and a wall-clock step
// between them does not change the verdict; on a clock without one the
// comparison follows the step. A caller that holds the clock's monotonic
// reading of the moment the loss was observed uses
// [Manager.ClosePeerLostSince], the form matter.js's `asOf` has
// (Time.nowUs, SessionManager.ts:630-642 handlePeerShutdown and :645-647
// handlePeerLoss at 862ad75f).
func (m *Manager) ClosePeerLost(fabricIndex uint8, peerNodeID uint64, asOf time.Time) int {
	return m.closePeerLost(fabricIndex, peerNodeID, func(entry *Entry) bool {
		return !asOf.IsZero() && !entry.CreatedAt().Before(asOf)
	})
}

// ClosePeerLostSince is [Manager.ClosePeerLost] with the cutoff on the
// clock's monotonic reading ([clock.MonotonicOf], [Entry.CreatedAtMonotonic]):
// every live session of the peer created before sinceMono is force-closed,
// one created at or after it is kept — matter.js keeps "sessions created
// at or after this instant", its asOf being Time.nowUs, the monotonic
// reading (SessionManager.ts:623-642 handlePeerShutdown, :688
// #handlePeerLoss at 862ad75f), so a wall-clock step between the session's
// creation and the loss neither spares nor condemns it. Returns the number
// of sessions closed.
func (m *Manager) ClosePeerLostSince(fabricIndex uint8, peerNodeID uint64, sinceMono time.Duration) int {
	return m.closePeerLost(fabricIndex, peerNodeID, func(entry *Entry) bool {
		return entry.CreatedAtMonotonic() >= sinceMono
	})
}

// closePeerLost is the sweep behind ClosePeerLost and ClosePeerLostSince:
// keep reports a session of the peer that survives the sweep.
func (m *Manager) closePeerLost(fabricIndex uint8, peerNodeID uint64, keep func(*Entry) bool) int {
	m.mu.Lock()
	victims := make([]*Entry, 0)
	for id, entry := range m.sessions {
		if entry.Session == nil {
			continue // a placeholder has no keys and no peer yet
		}
		if entry.FabricIndex() != fabricIndex || entry.Session.PeerNodeID() != peerNodeID {
			continue
		}
		if keep(entry) {
			continue // established after the loss was observed
		}
		victims = append(victims, entry)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	m.fireReannounceIfPeerGone(victims)
	return len(victims)
}

// MaxSessionsPerPeer is the number of sessions one peer may hold at a
// time. A new CASE does not close the peer's previous sessions, so a
// peer that reconnects repeatedly accumulates them; the cap is per
// peer rather than global so one noisy peer cannot evict another's.
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:185-189
// MAX_SESSIONS_PER_PEER.
const MaxSessionsPerPeer = 5

// evictExcessPeerSessionsLocked removes the least recently active
// sessions of added's peer from the live map once the peer holds more
// than [MaxSessionsPerPeer], and returns them for the caller to close
// OUTSIDE the lock (Session.Close acquires its own privacy mutex).
// Caller MUST hold m.mu in write mode and MUST have installed added.
//
// Only sessions the peer established claim a slot: initiator sessions
// are this node's to manage, PASE sessions never trigger the check,
// placeholders (Session==nil) are mid-handshake and left alone, and
// added itself is excluded so a burst of new sessions cannot evict the
// one that prompted the check. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:647-680
// #evictExcessSessionsFor and #leastRecentlyUsedSession.
//
// Only a session the peer established runs the check: one we initiated
// evicts nothing, since the peer loss our own traffic reports sweeps
// those. Mirrors matter.js packages/protocol/src/session/SessionManager.ts:272-277.
func (m *Manager) evictExcessPeerSessionsLocked(added *Entry) []*Entry {
	if added.isPase || added.isInitiator {
		return nil
	}
	fabricIndex, peerNodeID := added.FabricIndex(), added.Session.PeerNodeID()
	var claimants []*Entry
	for _, entry := range m.sessions {
		if entry == added || entry.Session == nil || entry.isInitiator {
			continue
		}
		if entry.FabricIndex() == fabricIndex && entry.Session.PeerNodeID() == peerNodeID {
			claimants = append(claimants, entry)
		}
	}
	excess := len(claimants) + 1 - MaxSessionsPerPeer
	if excess <= 0 {
		return nil
	}
	// Least recently active first on the monotonic reading, so a
	// wall-clock step between two sessions' traffic does not reorder
	// them; a session that never carried traffic sorts before every one
	// that did; the lower session id breaks a tie so the eviction order
	// is deterministic.
	sort.Slice(claimants, func(i, j int) bool { return lessRecentlyActive(claimants[i], claimants[j]) })
	evicted := claimants[:excess]
	for _, entry := range evicted {
		delete(m.sessions, entry.SessionID)
	}
	return evicted
}

// lessRecentlyActive orders a before b when a was active less recently, on
// the monotonic reading, so a wall-clock step between two sessions' traffic
// does not reorder them; a session that never carried traffic sorts before
// every one that did; the lower session id breaks a tie so the order is
// deterministic.
func lessRecentlyActive(a, b *Entry) bool {
	ta, oka := a.lastActivityMonotonic()
	tb, okb := b.lastActivityMonotonic()
	switch {
	case oka != okb:
		return !oka
	case ta != tb:
		return ta < tb
	}
	return a.SessionID < b.SessionID
}

// closeStaleEntries shuts down each session outside any lock. Safe
// against the empty / nil slice — turning the eviction call sites
// into one-liners.
func closeStaleEntries(entries []*Entry) {
	for _, e := range entries {
		if e == nil || e.Session == nil {
			continue
		}
		e.Session.Close()
	}
}

// Active returns the count of live sessions.
func (m *Manager) Active() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// StartReaper launches a background goroutine that evicts sessions
// idle for longer than idleTimeout. The goroutine stops when ctx is
// cancelled. poll controls how often the sweep runs; a reasonable
// default is idleTimeout/2.
//
// Sessions with a nil channel (pre-allocated placeholders whose
// handshake has not completed) are skipped so that in-progress
// commissioning handshakes are not torn down. A session where
// LastActivity is the zero time (opened, no traffic yet) is measured
// from its creation instead. A session the reap exemption spares
// ([Manager.SetReapExemption]) — the bridge's: one that carries a live
// subscription — stays however long it is idle.
//
// The manager owns the goroutine: [Manager.StopReaper] cancels it and
// waits for it to return. One reaper runs at a time; a second StartReaper
// without a StopReaper in between is a no-op, as for the bridge's
// per-exchange providers (PerExchangeCaseProvider.StartReaper).
//
// Mirrors the session-eviction logic in matter.js
// packages/protocol/src/session/SessionManager.ts and the
// SessionInactiveBaseInterval defined in Matter §4.13.2.6.1.
func (m *Manager) StartReaper(ctx context.Context, idleTimeout, poll time.Duration) {
	m.reaperMu.Lock()
	defer m.reaperMu.Unlock()
	if m.reaperStop != nil {
		return
	}
	rctx, cancel := context.WithCancel(ctx)
	m.reaperStop = cancel
	ticker := m.clk().NewTicker(poll)
	m.reaperWG.Go(func() {
		defer ticker.Stop()
		for {
			select {
			case <-rctx.Done():
				return
			case <-ticker.C():
				m.reapIdle(idleTimeout)
			}
		}
	})
}

// StopReaper stops the idle reaper [Manager.StartReaper] started and
// waits for its goroutine to return; a sweep in progress finishes first.
// It is idempotent and safe without StartReaper. The sessions stay; a
// later StartReaper starts a new reaper.
func (m *Manager) StopReaper() {
	m.reaperMu.Lock()
	cancel := m.reaperStop
	m.reaperStop = nil
	m.reaperMu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.reaperWG.Wait()
}

// reapIdle collects and closes sessions whose last-activity timestamp
// is older than idleTimeout. Only entries with an established channel
// are considered. A session that never carried a message has no
// last-activity time; its idle clock starts at its creation, so it is
// reaped one idle timeout after it was installed (review II, W1-sec-06
// — before, such a session was exempt for good).
//
// A session the reap exemption spares ([Manager.SetReapExemption]) stays.
// The exemption is asked outside the manager's lock — it reaches into
// another component — so the idle sessions are collected first and taken
// out of the table afterwards, each only while it is still the entry
// under its id and still idle.
func (m *Manager) reapIdle(idleTimeout time.Duration) {
	now := m.clk().Now()
	cutoff := now.Add(-idleTimeout)
	placeholderCutoff := now.Add(-PlaceholderIDTTL)
	type idleEntry struct {
		id uint16
		e  *Entry
	}
	m.mu.Lock()
	var idle []idleEntry
	for id, e := range m.sessions {
		if e.Session == nil {
			// Placeholder for a handshake that has not opened its
			// session yet. Young ones belong to a handshake still in
			// flight; past [PlaceholderIDTTL] nothing can claim the id
			// any more, so it goes back into circulation. There is no
			// session to close and no peer to notify — the entry is
			// dropped silently rather than routed through the close
			// cascade.
			if !e.allocatedAt.IsZero() && e.allocatedAt.Before(placeholderCutoff) {
				delete(m.sessions, id)
			}
			continue
		}
		if idleSince(e, cutoff) {
			idle = append(idle, idleEntry{id: id, e: e})
		}
	}
	exempt := m.reapExempt
	m.mu.Unlock()
	if len(idle) == 0 {
		return
	}
	if exempt != nil {
		idle = slices.DeleteFunc(idle, func(c idleEntry) bool { return exempt(c.id) })
	}
	m.mu.Lock()
	victims := make([]*Entry, 0, len(idle))
	for _, c := range idle {
		if m.sessions[c.id] != c.e || !idleSince(c.e, cutoff) {
			continue // closed, replaced or active again meanwhile
		}
		victims = append(victims, c.e)
		delete(m.sessions, c.id)
	}
	m.mu.Unlock()
	// Tell each reaped peer its session is gone while the keys can
	// still seal the CloseSession StatusReport, then resume mDNS
	// broadcast for peers left with zero sessions so they rediscover
	// the bridge instead of retransmitting into a dead session.
	// Mirrors matter.js ExchangeManager.ts:693/716-726 (gracefulClose →
	// #sendCloseSession) + DeviceAdvertiser.ts:132-149.
	m.notifyGracefulClose(victims, time.Time{})
	closeStaleEntries(victims)
	m.fireOnSessionClose(victims)
	m.fireReannounceIfPeerGone(victims)
}

// idleSince reports whether e's last activity — its creation when it
// never carried a message (a commissioning still in flight is inside the
// timeout) — lies before cutoff. An entry without either time is never
// idle.
func idleSince(e *Entry, cutoff time.Time) bool {
	la := e.LastActivity()
	if la.IsZero() {
		la = e.CreatedAt()
		if la.IsZero() {
			return false
		}
	}
	return la.Before(cutoff)
}

// PersistResumption stores a resumption-id pre-shared secret so a
// returning peer can resume via Sigma1.ResumptionID. resumptionID
// must be 16 bytes; sharedSecret must be 32 bytes. peerCATs carries
// the CASE Authenticated Tags from the peer's verified NOC — the
// resume path re-grants them without re-validating the NOC, so
// dropping them here silently strips CAT-scoped ACL privilege from
// every resumed session. Wired in daemon.go (matter init) and called
// from both CASE-onEstablished paths after every successful
// OpenFromSigmaWithID. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:210
// `this.#sessions.saveResumptionRecord(cx.resumptionRecord)`.
func (m *Manager) PersistResumption(ctx context.Context, fabricIndex uint8, peerNodeID uint64, resumptionID, sharedSecret []byte, peerCATs []uint32) error {
	return m.store.UpsertResumption(ctx, store.ResumptionRecord{
		FabricIndex:  fabricIndex,
		PeerNodeID:   peerNodeID,
		ResumptionID: resumptionID,
		SharedSecret: sharedSecret,
		CASEAuthTags: peerCATs,
	})
}

// LookupResumption finds the resumption record by id only.
func (m *Manager) LookupResumption(ctx context.Context, resumptionID []byte) (store.ResumptionRecord, error) {
	return m.store.GetResumptionByID(ctx, resumptionID)
}

// GenerateResumptionID returns a fresh 16-byte random id from
// crypto/rand. Production callers use this to assign Sigma2's
// ResumptionID field.
func GenerateResumptionID() ([]byte, error) {
	out := make([]byte, 16)
	if _, err := rand.Read(out); err != nil {
		return nil, fmt.Errorf("operational: generate resumption id: %w", err)
	}
	return out, nil
}

// allocateIDLocked walks 1..0xFFFE for the next free session id. The
// caller must hold m.mu in write mode.
//
// When every id is taken it reclaims the oldest placeholder — an id
// staked for a handshake that never completed — rather than refusing
// the allocation, so a burst of aborted handshakes cannot lock out
// every later CASE exchange. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:537
// (`getNextAvailableSessionId` falls back to `findOldestInactiveSession`
// and reuses its id). We stop at placeholders and never evict a live
// session here: closing one has to run its graceful-close cascade
// outside this lock, so [Manager.AllocateID] does that after this returns
// [ErrSessionExhausted] (review II, R2-P3-admin-05); see
// notes/parity/by_design.md BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly.
func (m *Manager) allocateIDLocked() (uint16, error) {
	const maxID = uint16(SessionIDSpace)
	// The table holds ids 1..maxID only, so below that size an id is free
	// and the walk finds it; at that size there is none to find, and the
	// walk is skipped (review II, R2-P3-lan-dev-02).
	if len(m.sessions) < int(maxID) {
		for range maxID {
			id := m.nextID
			m.nextID++
			if m.nextID == 0 || m.nextID > maxID {
				m.nextID = 1
			}
			if _, in := m.sessions[id]; !in && id != 0 {
				return id, nil
			}
		}
	}
	// The oldest placeholder, from the front of the queue AllocateID
	// appends to in allocation order: references that no longer stake
	// their id are dropped on the way, each once.
	for len(m.placeholders) > 0 {
		ref := m.placeholders[0]
		m.placeholders = m.placeholders[1:]
		if m.stakes(ref) {
			delete(m.sessions, ref.id)
			return ref.id, nil
		}
	}
	return 0, ErrSessionExhausted
}

// SessionInfo is a read-only view of one secure session, for operator
// diagnostics. It deliberately carries no key material, no attestation
// challenge and no session object — only what answers "is this
// controller still talking to us, and when did it last".
type SessionInfo struct {
	// SessionID is the local 16-bit identifier carried in the message
	// header of every encrypted exchange with this peer.
	SessionID uint16
	// FabricIndex is the fabric the session belongs to. Zero means a
	// PASE session, which exists only during commissioning.
	FabricIndex uint8
	// PeerNodeID / LocalNodeID identify the two ends of the session.
	PeerNodeID  uint64
	LocalNodeID uint64
	// IsPASE marks a session from the commissioning handshake rather
	// than an operational (CASE) one. It survives the fabric adoption
	// that AddNOC performs, so it stays true for the commissioning
	// session even once it carries a fabric index.
	IsPASE bool
	// LastActivity is when this session last carried traffic in either
	// direction; LastPeerActivity only counts what the peer sent. The
	// difference is what distinguishes "we keep reporting into a void"
	// from "the controller is still there" — an idle peer with recent
	// local activity is exactly the shape of a controller that went
	// away without closing its session.
	LastActivity     time.Time
	LastPeerActivity time.Time
	// PeerIdleIntervalMs, PeerActiveIntervalMs and PeerActiveThresholdMs
	// are the intervals the peer advertised in its handshake
	// ([Entry.PeerMRPIntervals]); 0 when it advertised none.
	PeerIdleIntervalMs, PeerActiveIntervalMs, PeerActiveThresholdMs uint32
}

// SessionTableOccupancy is how much of the session-id space the bridge
// currently holds, split by what holds it.
//
// The split is the point. A reserved id is one [Manager.AllocateID]
// staked for a handshake that announced it in Sigma2 and never reached
// key derivation; it appears in no session list, survives for
// [PlaceholderIDTTL], and is the only shape in which the 16-bit space
// actually drains. Reporting it as a session would claim a controller is
// connected; leaving it out entirely hides the drain.
type SessionTableOccupancy struct {
	// Live counts sessions with key material installed.
	Live int
	// Reserved counts ids staked by a handshake that has not completed.
	Reserved int
	// Capacity is the size of the allocator's id space.
	Capacity int
}

// Free returns the ids neither a live session nor a staked handshake
// holds. Never negative.
func (o SessionTableOccupancy) Free() int {
	free := o.Capacity - o.Live - o.Reserved
	if free < 0 {
		return 0
	}
	return free
}

// Occupancy returns a snapshot of the session table's id usage.
func (m *Manager) Occupancy() SessionTableOccupancy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	occ := SessionTableOccupancy{Capacity: SessionIDSpace}
	for _, e := range m.sessions {
		switch {
		case e == nil:
			continue
		case e.Session == nil:
			occ.Reserved++
		default:
			occ.Live++
		}
	}
	return occ
}

// SessionIDsForPeer returns the ids of the live sessions currently bound
// to (fabricIndex, peerNodeID), ordered by id. Staked ids are excluded:
// they carry no peer.
//
// Because opening a session evicts that peer's earlier sessions
// ([Manager.collectStalePeerSessionsLocked]), the ids this reports
// immediately before an open are the ones the open displaces — which is
// what the CASE resume record needs, since a resume that reuses the
// announced session id displaces the very session it is resuming.
func (m *Manager) SessionIDsForPeer(fabricIndex uint8, peerNodeID uint64) []uint16 {
	m.mu.RLock()
	var out []uint16
	for _, e := range m.sessions {
		if e == nil || e.Session == nil {
			continue
		}
		if e.FabricIndex() == fabricIndex && e.Session.PeerNodeID() == peerNodeID {
			out = append(out, e.SessionID)
		}
	}
	m.mu.RUnlock()
	slices.Sort(out)
	return out
}

// Sessions returns a snapshot of every open session, ordered by session
// id so repeated calls read consistently.
//
// It exists because the daemon tracks per-session liveness that nothing
// could see: the reaper consumed it internally and no operator surface
// could answer whether a controller was still connected. The snapshot is
// a copy — callers cannot reach the session keys through it.
func (m *Manager) Sessions() []SessionInfo {
	m.mu.RLock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, e := range m.sessions {
		if e == nil || e.Session == nil {
			continue
		}
		e.mu.Lock()
		info := SessionInfo{
			SessionID:        e.SessionID,
			FabricIndex:      e.fabricIndex,
			PeerNodeID:       e.Session.PeerNodeID(),
			LocalNodeID:      e.Session.LocalNodeID(),
			IsPASE:           e.isPase,
			LastActivity:     wallAsNow(e.clock, e.lastActivity, e.lastActivityMono),
			LastPeerActivity: wallAsNow(e.clock, e.lastPeerActivity, e.lastPeerActivityMono),

			PeerIdleIntervalMs:    e.peerIdleIntervalMs,
			PeerActiveIntervalMs:  e.peerActiveIntervalMs,
			PeerActiveThresholdMs: e.peerActiveThresholdMs,
		}
		e.mu.Unlock()
		out = append(out, info)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// wallAsNow is stamp as the wall clock reads it now: the monotonic
// reading mono carried over the clock's current wall-clock offset, so a
// step since the stamp moves the exposed time with the clock — matter.js
// Session.ts:154-162 wallClockActivity. A zero stamp (no traffic yet)
// stays zero.
func wallAsNow(clk clock.Clock, stamp time.Time, mono time.Duration) time.Time {
	if stamp.IsZero() {
		return time.Time{}
	}
	return clock.WallOf(clk, mono)
}
