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
	"sync/atomic"
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

	// establishMu orders the establishments of CASE sessions: held from
	// before the computation under mu until the supersession hooks of that
	// establishment ran, so the hooks of one establishment all run before
	// those of the next, as matter.js emits them synchronously on one
	// thread (packages/protocol/src/session/SessionManager.ts:750-769 at
	// b25ed960, #sessionEstablished; F-b25ed960-40). Released before the
	// per-peer eviction closes anything — matter.js queues that eviction
	// (:764-768). Lock order: establishMu, then mu; never the reverse.
	establishMu sync.Mutex

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
	// onSessionSuperseded fires once per older session a newly
	// established one supersedes (SetOnSessionSuperseded).
	onSessionSuperseded func(olderID, newerID uint16)
	// testBeforeSupersede, set by tests only, runs at every establishment
	// between the table's unlock and the supersession hooks.
	testBeforeSupersede func(newer *Entry)
	// byPeer indexes the CASE sessions installed by their peer node id, so
	// the supersession check at an establishment reads the peer's sessions
	// rather than the whole table. Entries are added at the install and
	// removed with the entry ([Manager.removeLocked], and for an occupant
	// replaced in place [Manager.installEntryLocked]); a read of a bucket
	// (livePeerBucketLocked) still filters on the table as the invariant's
	// check. Guarded by mu.
	byPeer map[uint64][]*Entry
	// registrations counts the keyed sessions installed so far: the next
	// one's registration position ([Entry].registration). Incremented
	// under mu at the install, read without it by
	// [Manager.RegistrationCount]. Mirrors matter.js
	// packages/protocol/src/session/SessionManager.ts:1266-1284 at b25ed960
	// (RegisteredSessions.add: #positions.set(session, this.#count++)).
	registrations atomic.Uint64

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
	// db021da0 (sessions.deleted, per deleted session: skip when
	// `fabric.hasSessionForPeer(...)`, Fabric.ts:414-420, otherwise
	// `advertisementForFabric(fabric).serviceDisconnected()` for that
	// fabric only, Advertiser.ts:69-75). Not
	// fired by [Manager.CloseAllGraceful]: matter.js kicks off the
	// advertiser shutdown before removing sessions precisely to prevent
	// re-announces during teardown (packages/node/src/behavior/system/
	// network/ServerNetworkRuntime.ts:450-453 at b25ed960).
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

// hasSessionForPeer reports whether any live established session matches
// the (fabricIndex, peerNodeID) pair. Mirrors the guard matter.js applies
// before resuming operational broadcast on session deletion
// (DeviceAdvertiser.ts:138 `fabric.hasSessionForPeer(...)`), which skips a
// session still establishing (packages/protocol/src/fabric/Fabric.ts:422-429
// at b25ed960).
func (m *Manager) hasSessionForPeer(fabricIndex uint8, peerNodeID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.sessions {
		if e == nil || e.Session == nil || e.establishing {
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
// at b25ed960, which runs the check per session deleted from the
// established set (`sessions.sessions.deleted`, :132): a victim that was
// still establishing never joined it and triggers nothing.
func (m *Manager) fireReannounceIfPeerGone(victims []*Entry) {
	m.mu.RLock()
	hook := m.onReannounce
	established := make([]*Entry, 0, len(victims))
	for _, e := range victims {
		if e != nil && e.Session != nil && !e.establishing {
			established = append(established, e)
		}
	}
	m.mu.RUnlock()
	if hook == nil {
		return
	}
	type fabricPeer struct {
		fabricIndex uint8
		peerNodeID  uint64
	}
	var gone []fabricPeer
	for _, e := range established {
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
// at db021da0). Passing nil clears any prior hook; the callback fires
// outside the manager's lock.
func (m *Manager) SetOnPaseSessionClose(fn func(sessionID uint16)) {
	m.mu.Lock()
	m.onPaseSessionClose = fn
	m.mu.Unlock()
}

// SetOnSessionSuperseded registers a callback invoked once per older
// session a newly established CASE session supersedes, with the older and
// the newer session's ids, outside the manager's lock. It fires when the
// newer session is established — at its install for one installed
// established, at the end of its handshake for a responder session
// installed establishing — and before the sessions that establishment
// evicts from the per-peer cap are closed, so a subscription moved by the
// callback has left a session before the eviction closes it. Supersession
// closes nothing: the older session stays open until it is evicted, its
// peer is lost or it is closed.
//
// A newer session supersedes an older one when both are CASE sessions on
// the same fabric with the same peer node id and the same set of CASE
// Authenticated Tags (order and repetition free), whoever initiated
// either; an older session still establishing, or no longer in the table
// (closed), is not superseded. Older sessions are reported in the order
// they were created.
//
// The hooks of one establishment all run before those of the next
// ([Manager.establishMu]), as matter.js emits them synchronously on one
// thread: a subscription moved off a session another establishment just
// evicted is not left behind on a superseded one (F-b25ed960-40). The hook
// therefore must not open or complete a session on this Manager, and a
// caller must not establish one while holding a lock the hook takes; a
// hook that blocks holds up every establishment of the Manager.
//
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:750-769
// at b25ed960 (#sessionEstablished: supersession first and synchronous,
// then the per-peer eviction) and :1233-1250 (supersedes, hasEqualCats),
// chip src/transport/SecureSessionTable.h:100-120 at 6170af84
// (NewerSessionAvailable). Passing nil clears any prior hook.
func (m *Manager) SetOnSessionSuperseded(fn func(olderID, newerID uint16)) {
	m.mu.Lock()
	m.onSessionSuperseded = fn
	m.mu.Unlock()
}

// supersededByLocked returns the established sessions newer supersedes,
// oldest first. Caller MUST hold m.mu for writing and MUST call it only once newer, a
// CASE session, is installed and established. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:757-763 at b25ed960
// (the loop over the established sessions) and :1242-1250 (supersedes).
// Unlike matter.js the PASE check covers older too: this table does not
// guarantee a PASE session's unspecified peer node id, which matter.js
// relies on for older (:1239-1241).
func (m *Manager) supersededByLocked(newer *Entry) []*Entry {
	fabricIndex, peerNodeID := newer.FabricIndex(), newer.Session.PeerNodeID()
	cats := newer.Session.PeerCATs()
	bucket := m.livePeerBucketLocked(peerNodeID)
	var older []*Entry
	for _, e := range bucket {
		if e == newer || e.Session == nil || e.establishing || e.isPase {
			continue
		}
		if e.FabricIndex() != fabricIndex || e.Session.PeerNodeID() != peerNodeID || !equalCATSets(cats, e.Session.PeerCATs()) {
			continue
		}
		older = append(older, e)
	}
	sort.Slice(older, func(i, j int) bool {
		a, b := older[i], older[j]
		if a.createdAtMono != b.createdAtMono {
			return a.createdAtMono < b.createdAtMono
		}
		return a.SessionID < b.SessionID
	})
	return older
}

// indexPeerLocked adds e, a CASE session just installed, to the peer
// index. Caller holds m.mu.
func (m *Manager) indexPeerLocked(e *Entry) {
	if m.byPeer == nil {
		m.byPeer = map[uint64][]*Entry{}
	}
	peer := e.Session.PeerNodeID()
	m.byPeer[peer] = append(m.byPeer[peer], e)
}

// livePeerBucketLocked returns the indexed sessions of peerNodeID that
// still hold their id, pruning the others. Caller holds m.mu for writing.
func (m *Manager) livePeerBucketLocked(peerNodeID uint64) []*Entry {
	bucket := m.byPeer[peerNodeID]
	live := bucket[:0]
	for _, e := range bucket {
		if m.sessions[e.SessionID] == e {
			live = append(live, e)
		}
	}
	clear(bucket[len(live):])
	if len(live) == 0 {
		delete(m.byPeer, peerNodeID)
		return nil
	}
	m.byPeer[peerNodeID] = live
	return live
}

// unindexPeerLocked removes e, a session leaving the table, from the peer
// index: by identity, freeing its slot, and the peer's bucket with its
// last session. A placeholder (no session) was never indexed. Caller holds
// m.mu for writing.
func (m *Manager) unindexPeerLocked(e *Entry) {
	if e == nil || e.Session == nil {
		return
	}
	peer := e.Session.PeerNodeID()
	bucket := m.byPeer[peer]
	i := slices.Index(bucket, e)
	if i < 0 {
		return
	}
	bucket = slices.Delete(bucket, i, i+1)
	if len(bucket) == 0 {
		delete(m.byPeer, peer)
		return
	}
	m.byPeer[peer] = bucket
}

// removeLocked takes e, the entry under id, out of the table and out of
// the peer index in one step, so no removal leaves the index behind
// (F-b25ed960-41). A keyed session leaves at once, as matter.js drops a
// closed session from its sets at the end of its close
// (packages/protocol/src/session/NodeSession.ts:485-504 at b25ed960,
// registeredSessions.delete :503; SessionManager.ts:333-335). A
// placeholder's removal ([Manager.ReleaseStake], the reaper's and the
// allocator's) may delete the id directly: it was never indexed. Caller
// holds m.mu for writing.
func (m *Manager) removeLocked(id uint16, e *Entry) {
	delete(m.sessions, id)
	m.unindexPeerLocked(e)
}

// equalCATSets reports whether a and b hold the same tags as sets.
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:1246-1250
// at b25ed960 (hasEqualCats).
func equalCATSets(a, b []uint32) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	for _, x := range b {
		if !slices.Contains(a, x) {
			return false
		}
	}
	return true
}

// fireOnSessionSuperseded dispatches the supersession hook for every
// entry of older, superseded by newer. Caller MUST hold no manager locks.
func (m *Manager) fireOnSessionSuperseded(older []*Entry, newer *Entry) {
	if len(older) == 0 {
		return
	}
	m.mu.RLock()
	hook := m.onSessionSuperseded
	m.mu.RUnlock()
	if hook == nil {
		return
	}
	for _, e := range older {
		hook(e.SessionID, newer.SessionID)
	}
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
// reaching [Manager.OpenFromSigmaWithStake]. Past it the reaper hands the
// slot back.
//
// matter.js has no equivalent because it stakes nothing:
// `getNextAvailableSessionId`
// (packages/protocol/src/session/SessionManager.ts:581-599 at db021da0)
// returns the next id no session holds and moves its cursor on
// (#takeSessionId, :602-606); a handshake takes its id before its
// session exists (the CASE responder for Sigma2, case/CaseServer.ts:266,
// the session created at :316) and nothing records the id until then.
// Our CASE responder stakes the id it announces in Sigma2, so an aborted
// handshake leaves an id behind and something has to reclaim it —
// otherwise a peer that opens exchanges and never answers Sigma2 walks
// the daemon through all 65534 slots and every later CASE handshake,
// legitimate ones included, is refused until a restart.
//
// The value must outlive every window in which the staked id can still
// be claimed, because reclaiming one early would hand a live handshake
// an id another session already owns. Both stakes live in a per-exchange
// handshake adapter: the CASE responder's, and the PASE responder's,
// which node stakes when it builds the exchange's adapter
// (buildPaseAdapter / buildPaseAdapterFromContext in node/security.go),
// not when a commissioning window opens. Past the adapter TTL
// (handshakeAdapterTTL, 60 s) the handshake can never complete, so the
// 20 minutes leave a wide margin (review II, R2-P7-sec-05).
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
	// establishing marks a CASE responder session whose handshake has not
	// completed: a new session until its Success StatusReport went out for
	// the first time, a resumed one until the initiator's Success arrived
	// ([Manager.completeEstablishment]). It receives and routes messages —
	// the table's lookups, closes, peer loss and teardown see it — but the
	// pickers ([Manager.NewestSessionForPeer], [Manager.SessionIDsForPeer]),
	// the session listing ([Manager.Sessions]), the peer-presence check
	// behind the reannounce and the per-peer cap do not. Mirrors matter.js
	// packages/protocol/src/session/NodeSession.ts:209-231 at b25ed960 (the
	// isEstablishing table). Guarded by the manager's mu, written only
	// while the entry holds its id.
	establishing bool
	// registration is the session's registration position: the value of
	// [Manager.RegistrationCount] when it entered the table with its keys
	// ([Manager.registerLocked]), an establishing CASE responder session
	// included. Written once under the manager's mu before the entry is
	// visible, never rewritten, and kept after the session left the table.
	// 0 and meaningless for a placeholder, which no sweep visits. Mirrors
	// matter.js packages/protocol/src/session/SessionManager.ts:1258 at
	// b25ed960 (#positions, a WeakMap nothing deletes from).
	registration uint64
	// createdAt is when the session was installed with its keys. The
	// deprecated time form of the peer-loss sweep ([Manager.ClosePeerLost])
	// spares sessions created at or after its instant (matter.js
	// packages/protocol/src/session/SessionManager.ts:832-834 at b25ed960).
	createdAt time.Time
	// lastActivityMono, lastPeerActivityMono and createdAtMono are the
	// same stamps on the clock's monotonic reading (clock.MonotonicOf): the
	// readings every interval is judged on — the peer-active threshold,
	// the per-peer LRU eviction — so a wall-clock step neither ages nor
	// rejuvenates a session. The wall times above are what the stamps
	// read as, for [SessionInfo]. matter.js keeps its timestamps on
	// Time.nowUs, the monotonic reading (Session.ts:142-152 at db021da0),
	// and exposes them through the wall-clock offset (:155-163).
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
// `maxTcpMessageSize` on the Session (Session.ts:87, :119 at db021da0).
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
// defaults. Mirrors the selection of matter.js MRP.ts:162-190
// retransmissionIntervalOf at db021da0 (:181 "baseInterval = (isPeerActive
// ? activeInterval : idleInterval) + additionalDelay", re-evaluated per
// transmission; the additionalDelay term is the caller's, see the tracker's
// margin in bridge/outbound_reliable.go) and chip GetMRPBaseTimeout.
func (e *Entry) RetransmitBaseInterval(_ time.Time) time.Duration {
	idle, active, _, peerActive := e.PeerMRPSchedule()
	if peerActive {
		return active
	}
	return idle
}

// PeerMRPSchedule returns the peer's MRP schedule as a sender judges it
// now: its idle and active intervals and its active threshold, each the
// spec default where the peer advertised none, and whether the peer is
// active — it sent a message within its active threshold.
// [Entry.RetransmitBaseInterval] picks one interval of it; a bound on a
// wait for the peer's answer sums the whole schedule (review II,
// F-db021da0-13).
func (e *Entry) PeerMRPSchedule() (idle, active, threshold time.Duration, peerActive bool) {
	// The peer-active decision is an interval, judged on the clock's
	// monotonic reading so a wall-clock step cannot flip it (matter.js
	// Session.ts:149-151 on Time.nowUs).
	e.mu.Lock()
	lastPeer, lastPeerMono := e.lastPeerActivity, e.lastPeerActivityMono
	idleMs, activeMs, thresholdMs := e.peerIdleIntervalMs, e.peerActiveIntervalMs, e.peerActiveThresholdMs
	e.mu.Unlock()
	nowMono := clock.MonotonicOf(e.clock)

	idle = mrp.SessionIdleIntervalDefault
	if idleMs != 0 {
		idle = time.Duration(idleMs) * time.Millisecond
	}
	active = mrp.SessionActiveIntervalDefault
	if activeMs != 0 {
		active = time.Duration(activeMs) * time.Millisecond
	}
	threshold = mrp.SessionActiveThresholdDefault
	if thresholdMs != 0 {
		threshold = time.Duration(thresholdMs) * time.Millisecond
	}
	return idle, active, threshold, !lastPeer.IsZero() && nowMono-lastPeerMono < threshold
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

// LastPeerActivity returns when the peer last sent on this session, as
// the wall clock reads it now: set when the session opened, moved by every
// inbound decrypt ([Entry.MarkActiveRx]) and never by this node's own
// sends. The zero time for an entry that never carried a peer.
func (e *Entry) LastPeerActivity() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return wallAsNow(e.clock, e.lastPeerActivity, e.lastPeerActivityMono)
}

// LastPeerActivityMonotonic is [Entry.LastPeerActivity] on the clock's
// monotonic reading ([clock.MonotonicOf]) — the reading
// [Manager.NewestSessionForPeer] orders by, which a wall-clock step does
// not move; false for an entry that never carried a peer.
func (e *Entry) LastPeerActivityMonotonic() (time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPeerActivityMono, !e.lastPeerActivity.IsZero()
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
	// ErrStakeLost is returned by [Manager.OpenFromSigmaWithStake],
	// [Manager.OpenFromSigmaAsInitiatorWithStake] and
	// [Manager.OpenFromPaseWithStake] when the [Stake] no
	// longer holds its id: the handshake released it, the reaper reclaimed
	// it past [PlaceholderIDTTL], or an allocation at a full table gave the
	// id to another handshake, which may have established its session
	// under it since. Nothing is installed and the id's holder stays as it
	// is (review II, N3-findev-1).
	ErrStakeLost = errors.New("operational: session id stake lost")
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
// packages/protocol/src/session/SessionManager.ts:204-206
// (MAX_SESSIONS_PER_PEER) and :779-810 (#evictExcessSessionsFor) at
// b25ed960; the session is installed established, so the check runs at
// once, as matter.js runs it for a session created without
// `establishing` (#sessionEstablished, :757-769, on RegisteredSessions.add,
// :1281-1283).
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

	m.establishMu.Lock()
	m.mu.Lock()
	id, err := m.allocateIDLocked()
	if err != nil {
		m.mu.Unlock()
		m.establishMu.Unlock()
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
	m.registerLocked(entry)
	m.sessions[id] = entry
	m.indexPeerLocked(entry)
	superseded := m.supersededByLocked(entry)
	excess := m.evictExcessPeerSessionsLocked(entry)
	m.mu.Unlock()
	if m.testBeforeSupersede != nil {
		m.testBeforeSupersede(entry)
	}
	// Supersession before the eviction closes anything: matter.js
	// SessionManager.ts:750-769 at b25ed960 (#sessionEstablished).
	m.fireOnSessionSuperseded(superseded, entry)
	m.establishMu.Unlock()
	// Force close, no CloseSession: matter.js SessionManager.ts:779-808
	// at b25ed960 (#evictExcessSessionsFor, a forced close) skips the
	// graceful path for the cap because the eviction target is typically
	// the peer it can no longer reach.
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
//
// Deprecated: the id comes back unbound to its stake: at a full table a
// later allocation may give it to another handshake, and
// [Manager.ReleaseID] then frees that one's stake, and
// [Manager.OpenFromSigmaWithID] installs over it (review II, R2-P3-dr-dev-07,
// N3-findev-1, decision 160). Use [Manager.AllocateStake], whose [Stake]
// [Manager.ReleaseStake] and the …WithStake installs take; AllocateID is
// removable in v0.25.0.
func (m *Manager) AllocateID() (uint16, error) {
	s, err := m.AllocateStake()
	return s.ID, err
}

// Stake is a session id [Manager.AllocateStake] reserved for one
// handshake, and the reservation itself: [Manager.ReleaseStake] frees the
// id only while it still holds this reservation (review II,
// R2-P3-dr-dev-07). The zero Stake reserves nothing.
type Stake struct {
	// ID is the reserved session id.
	ID uint16
	e  *Entry
}

// AllocateStake is [Manager.AllocateID] returning the stake it made, for
// [Manager.ReleaseStake].
func (m *Manager) AllocateStake() (Stake, error) {
	m.mu.Lock()
	id, err := m.allocateIDLocked()
	// Every id held by a live session — one fabric's administrator can
	// open them for as many node ids as it issues NOCs: a session is
	// closed and its id reused, as matter.js closes
	// findOldestInactiveSession's session and takes its id
	// (packages/protocol/src/session/SessionManager.ts:590-598, :564-572
	// at db021da0). Which session is [Manager.leastRecentlyActiveLocked]'s
	// choice: inside the fabric holding the most (R2-P6-bound-0107). Its
	// close runs below, outside the lock (review II, R2-P3-admin-05).
	var evicted *Entry
	if errors.Is(err, ErrSessionExhausted) {
		if evicted = m.leastRecentlyActiveLocked(); evicted != nil {
			m.removeLocked(evicted.SessionID, evicted)
			id, err = evicted.SessionID, nil
		}
	}
	if err != nil {
		m.mu.Unlock()
		return Stake{}, err
	}
	defer m.closeEvicted(evicted)
	defer m.mu.Unlock()
	// Stake a placeholder so concurrent allocators do not hand the
	// same id out twice; OpenFromSigmaWithStake replaces it. The stamp
	// is what lets the reaper tell an in-flight handshake from one
	// that was abandoned — see [PlaceholderIDTTL].
	e := &Entry{SessionID: id, allocatedAt: m.clk().Now(), clock: m.clk()}
	m.sessions[id] = e
	m.placeholders = append(m.placeholders, placeholderRef{id: id, e: e})
	m.compactPlaceholdersLocked()
	return Stake{ID: id, e: e}, nil
}

// leastRecentlyActiveLocked returns the session a full id space gives up:
// the least recently active live session of the fabric that holds the
// most live sessions (PASE sessions and those not yet on a fabric count
// as fabric 0); between fabrics of equal size, the least recently active
// of all of them. nil when no live session exists. The order inside a
// fabric is [lessRecentlyActive]'s. Each session's fabric index is read
// once: [Manager.AdoptFabricIndex] changes it under the entry's lock, not
// the manager's, so a second read could count a session on one fabric
// and filter it on another.
//
// matter.js's findOldestInactiveSession filters by no fabric
// (SessionManager.ts:564-572, #leastRecentlyUsedSession :553-562 at
// db021da0), so one fabric that keeps its sessions busy would push
// another fabric's idle ones out. The per-fabric choice is ahead of
// matter.js (R2-P6-bound-0107): connectedhomeip evicts from the fabric
// that holds the most sessions first and uses last activity only as the
// final tie-break (src/transport/SecureSessionTable.cpp:228-337,
// DefaultEvictionPolicy, Key1 :239-244, Key6 :333-335 at 6170af84), in a
// pool sized for three CASE sessions per fabric (src/lib/core/CHIPConfig.h:808-821);
// it counts by GetFabricIndex() equality, sessions without a fabric
// included (SecureSessionTable.cpp:149-151). matter.js bounds sessions per
// peer only (packages/protocol/src/session/SessionManager.ts:201-205 at
// db021da0; MAX_SESSIONS_PER_PEER, :205, is that file's only cap). This
// choice takes chip's first and last key only; Key2 :246-257 (the hint's
// fabric), Key3 :259-265 (the peer holding the most), Key4 :286-303 (the
// hinted peer) and Key5 :305-330 (the session state) are not applied.
// Caller holds m.mu.
func (m *Manager) leastRecentlyActiveLocked() *Entry {
	type live struct {
		e      *Entry
		fabric uint8
	}
	sessions := make([]live, 0, len(m.sessions))
	held := make(map[uint8]int)
	most := 0
	for _, e := range m.sessions {
		if e.Session == nil {
			continue
		}
		f := e.FabricIndex()
		sessions = append(sessions, live{e, f})
		held[f]++
		most = max(most, held[f])
	}
	var oldest *Entry
	for _, l := range sessions {
		if held[l.fabric] != most {
			continue
		}
		if oldest == nil || lessRecentlyActive(l.e, oldest) {
			oldest = l.e
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
//
// Deprecated: ReleaseID frees whatever placeholder holds id, also the
// stake an allocation at a full table made for another handshake after it
// took the caller's (review II, R2-P3-dr-dev-07). Use [Manager.AllocateStake]
// and [Manager.ReleaseStake], which free the caller's own stake only;
// ReleaseID is removable in v0.25.0.
func (m *Manager) ReleaseID(id uint16) {
	m.mu.Lock()
	if e, ok := m.sessions[id]; ok && e.Session == nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

// ReleaseStake frees s's id when the handshake that staked it never
// reached OpenFromSigmaWithStake — only while the id still holds s's
// placeholder: an id an allocation at a full table gave to another
// handshake since, or one a session holds now, stays as it is.
func (m *Manager) ReleaseStake(s Stake) {
	m.mu.Lock()
	if m.stakes(placeholderRef{id: s.ID, e: s.e}) {
		delete(m.sessions, s.ID)
	}
	m.mu.Unlock()
}

// OpenFromSigmaWithStake is a variant of [Manager.OpenFromSigma] that
// registers the new session under the id s reserved (see
// [Manager.AllocateStake]), in place of s's placeholder.
//
// The install takes the stake, not the bare id: it registers the session
// only while s still holds its id, and otherwise returns [ErrStakeLost]
// and installs nothing. At a full table [Manager.AllocateStake] may have
// handed s's id to another handshake since; that handshake's stake, or
// the session it established under the id, stays as it is (review II,
// N3-findev-1). The zero Stake holds nothing and is refused.
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
func (m *Manager) OpenFromSigmaWithStake(s Stake, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(s.ID, &s, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, false, false)
}

// OpenFromSigmaAsInitiatorWithStake is [Manager.OpenFromSigmaWithStake]
// for a session this node opened as the CASE initiator: it encrypts with
// the I2R key and decrypts with R2I. The only initiator in this module
// re-establishes former subscriptions after a restart (docs/adr/0008);
// matter.js creates the same session from CaseClient.ts:#doPair with
// `isInitiator: true`.
func (m *Manager) OpenFromSigmaAsInitiatorWithStake(s Stake, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(s.ID, &s, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, true, false)
}

// OpenFromSigmaWithID is [Manager.OpenFromSigmaWithStake] under a bare id
// (see [Manager.AllocateID]). It overwrites any placeholder entry left
// by AllocateID.
//
// Deprecated: the install takes the id, not the [Stake]: it does not
// check that the id still holds this handshake's reservation. At a full
// table [Manager.AllocateStake] may have handed the id to another
// handshake since, and the install then overwrites that handshake's
// stake, or closes the session it already established under the id
// (review II, N3-findev-1). Use [Manager.AllocateStake] and
// [Manager.OpenFromSigmaWithStake], which install under the caller's own
// stake only; OpenFromSigmaWithID is removable in v0.25.0.
func (m *Manager) OpenFromSigmaWithID(id uint16, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(id, nil, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, false, false)
}

// OpenFromSigmaAsInitiatorWithID is [Manager.OpenFromSigmaWithID] for a
// session this node opened as the CASE initiator.
//
// Deprecated: as [Manager.OpenFromSigmaWithID], the install does not
// check the stake. Use [Manager.AllocateStake] and
// [Manager.OpenFromSigmaAsInitiatorWithStake];
// OpenFromSigmaAsInitiatorWithID is removable in v0.25.0.
func (m *Manager) OpenFromSigmaAsInitiatorWithID(id uint16, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (*Entry, error) {
	return m.openFromSigma(id, nil, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys, true, false)
}

// openFromSigma installs a CASE session under id. A non-nil stake must
// still hold id when the session is installed, or nothing is and the
// error is [ErrStakeLost]; a nil stake installs under the bare id, as
// [Manager.OpenFromSigmaWithID] does.
//
// establishing installs a responder session whose handshake has not
// completed (see [Entry] establishing): it routes, but no picker returns it
// and the per-peer cap neither counts nor runs for it until
// [Manager.completeEstablishment]. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:177-190 and :314-327 at
// b25ed960 (createSecureSession with `establishing: true`) and
// SessionManager.ts:1266-1284 (RegisteredSessions.add: an establishing
// session joins the established set on its established event only).
func (m *Manager) openFromSigma(id uint16, stake *Stake, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys, initiator, establishing bool) (*Entry, error) {
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
		establishing:         establishing && !initiator,
		createdAt:            m.clk().Now(),
		// The peer counts active from the session's creation: matter.js
		// NodeSession.ts:121 at db021da0 ("We always set the active timestamp for Secure
		// sessions"), chip SecureSession.h:349 (mLastPeerActivityTime = now).
		lastPeerActivity:     m.clk().Now(),
		lastPeerActivityMono: clock.MonotonicOf(m.clk()),
		createdAtMono:        clock.MonotonicOf(m.clk()),
		clock:                m.clk(),
	}
	// An establishing install supersedes nothing yet: only an
	// establishment takes the establishment order ([Manager.establishMu]).
	// The occupant's close hooks below — and its CloseSession — then run
	// inside it too; the hooks must not establish on this manager either.
	ordered := !entry.establishing
	if ordered {
		m.establishMu.Lock()
	}
	unorder := func() {
		if ordered {
			m.establishMu.Unlock()
		}
	}
	m.mu.Lock()
	// A stake that no longer holds its id installs nothing: the id's
	// holder now — another handshake's stake or its session — stays
	// (N3-findev-1). The check and the install share the lock.
	if stake != nil && !m.stakes(placeholderRef{id: stake.ID, e: stake.e}) {
		m.mu.Unlock()
		unorder()
		sess.Close()
		return nil, ErrStakeLost
	}
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
	m.indexPeerLocked(entry)
	// The per-peer cap runs once the session is established: here for one
	// installed established, at [Manager.completeEstablishment] for an
	// establishing one (matter.js SessionManager.ts:757-769 at b25ed960,
	// #sessionEstablished on the established set's added event, :336).
	var superseded, excess []*Entry
	if !entry.establishing {
		superseded = m.supersededByLocked(entry)
		excess = m.evictExcessPeerSessionsLocked(entry)
	}
	m.mu.Unlock()
	m.notifyGracefulClose(occupied, time.Time{})
	closeStaleEntries(occupied)
	m.fireOnSessionClose(occupied)
	// Supersession after the occupant's close — which closes what is
	// keyed to this id — and before the eviction closes the sessions the
	// moved subscriptions leave (matter.js SessionManager.ts:750-769 at
	// b25ed960, #sessionEstablished).
	if m.testBeforeSupersede != nil && ordered {
		m.testBeforeSupersede(entry)
	}
	m.fireOnSessionSuperseded(superseded, entry)
	unorder()
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
	m.registerLocked(entry)
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

// OpenFromPaseWithStake is a variant of [Manager.OpenFromPase] that
// registers the new PASE session under the id s reserved (see
// [Manager.AllocateStake]), in place of s's placeholder.
//
// Use this when the responder session id must be known BEFORE the
// PBKDFParamResponse is sent — the commissioner echoes it back as
// Header.SessionID on every post-PASE-establishment packet (IM reads,
// writes, invokes). Pre-allocating the id and embedding it in the
// PBKDFParamResponse guarantees the bridge's session lookup resolves
// the correct entry for every subsequent datagram, even when other
// sessions occupy the lower-numbered slots.
//
// The install takes the stake, not the bare id, with the contract of
// [Manager.OpenFromSigmaWithStake]: it registers the session only while
// s still holds its id, and otherwise returns [ErrStakeLost] and
// installs nothing; another handshake's stake, or the session it
// established under the id, stays as it is (review II, decision 159).
func (m *Manager) OpenFromPaseWithStake(s Stake, localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*Entry, error) {
	return m.openFromPase(s.ID, &s, localNodeID, peerNodeID, peerSessionID, sharedSecret)
}

// OpenFromPaseWithID is [Manager.OpenFromPaseWithStake] under a bare id
// (see [Manager.AllocateID]). It overwrites any placeholder entry left by
// AllocateID.
//
// Deprecated: the install takes the id, not the [Stake]: at a full table
// [Manager.AllocateStake] may have handed the id to another handshake
// since, and the install then overwrites that handshake's stake, or
// closes the session it established under the id (review II, decision
// 159). Use [Manager.AllocateStake] and [Manager.OpenFromPaseWithStake],
// which install under the caller's own stake only; OpenFromPaseWithID is
// removable in v0.25.0.
func (m *Manager) OpenFromPaseWithID(id uint16, localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*Entry, error) {
	return m.openFromPase(id, nil, localNodeID, peerNodeID, peerSessionID, sharedSecret)
}

// openFromPase installs a PASE session under id, with openFromSigma's
// stake contract: a non-nil stake must still hold id, or nothing is
// installed and the error is [ErrStakeLost].
func (m *Manager) openFromPase(id uint16, stake *Stake, localNodeID, peerNodeID uint64, peerSessionID uint16, sharedSecret []byte) (*Entry, error) {
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
	if stake != nil && !m.stakes(placeholderRef{id: stake.ID, e: stake.e}) {
		m.mu.Unlock()
		sess.Close()
		return nil, ErrStakeLost
	}
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
// The install is the session's registration ([Manager.registerLocked]).
//
// The caller must hold m.mu in write mode.
func (m *Manager) installEntryLocked(id uint16, entry *Entry) *Entry {
	m.registerLocked(entry)
	displaced := m.sessions[id]
	m.sessions[id] = entry
	if displaced == nil || displaced.Session == nil {
		return nil
	}
	m.unindexPeerLocked(displaced)
	return displaced
}

// registerLocked gives entry the next registration position. Every keyed
// insert into the table calls it — [Manager.OpenFromSigma],
// [Manager.OpenFromPase] and the two that go through
// [Manager.installEntryLocked] — and nothing else: not the entry's
// construction (its creation time may precede a registration made
// meanwhile), not [Manager.completeEstablishment] (an establishing
// session registers at its install, as it routes from then on), not the
// placeholder [Manager.AllocateStake] stakes (it has no keys). The
// caller must hold m.mu in write mode.
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:1266-1284 at b25ed960
// (RegisteredSessions.add: the position is set before the establishing check),
// called from packages/protocol/src/session/NodeSession.ts:163-165 at b25ed960
// (the NodeSession constructor, once the keys are derived).
func (m *Manager) registerLocked(entry *Entry) {
	entry.registration = m.registrations.Add(1) - 1
}

// RegistrationCount is the number of keyed sessions the manager has
// registered so far — the registration position the next one gets. A
// caller that reads it when an exchange begins and later passes it to
// [Manager.ClosePeerLostBefore] closes the peer's sessions that existed
// then, the exchange's own among them, and spares every session that
// registered since, whatever its creation time. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:408-410 at b25ed960
// (registrationCount), read where an exchange's context is created
// (packages/protocol/src/protocol/ExchangeManager.ts:623 at b25ed960).
func (m *Manager) RegistrationCount() uint64 {
	return m.registrations.Load()
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
	m.removeLocked(sessionID, entry)
	m.mu.Unlock()
	m.finishClose(entry)
	return nil
}

// closeEntry is [Manager.Close] of e itself rather than of its id: it
// closes e only while e still holds its id, and reports whether it did.
// A caller that closes the session it installed a moment ago uses it, so
// an id another session or stake holds by then is left alone (review
// II, decision 159 / Fable Q1 record).
func (m *Manager) closeEntry(e *Entry) bool {
	if e == nil {
		return false
	}
	m.mu.Lock()
	if cur, ok := m.sessions[e.SessionID]; !ok || cur != e {
		m.mu.Unlock()
		return false
	}
	m.removeLocked(e.SessionID, e)
	m.mu.Unlock()
	m.finishClose(e)
	return true
}

// completeEstablishment ends the establishment of e, a responder session
// installed establishing: from now on the pickers return it and it counts
// for the per-peer cap, whose check runs now — the sessions it evicts are
// force-closed (no CloseSession) outside the lock, as at an install. It reports true when e is established — also when it
// already was and has been closed since — and false when e was closed
// while it was establishing, which therefore never becomes established.
// Mirrors matter.js packages/protocol/src/session/NodeSession.ts:241-251
// at b25ed960 (completeEstablishment: true if not establishing, false if
// closing) with the established event's consequences,
// SessionManager.ts:1274-1280 (the established set) and :757-769
// (#sessionEstablished, the per-peer cap); a closed entry is one no longer
// holding its id here, as this table has no closing state.
func (m *Manager) completeEstablishment(e *Entry) bool {
	if e == nil {
		return false
	}
	m.establishMu.Lock()
	m.mu.Lock()
	if !e.establishing {
		m.mu.Unlock()
		m.establishMu.Unlock()
		return true
	}
	if cur, ok := m.sessions[e.SessionID]; !ok || cur != e {
		m.mu.Unlock()
		m.establishMu.Unlock()
		return false
	}
	e.establishing = false
	superseded := m.supersededByLocked(e)
	excess := m.evictExcessPeerSessionsLocked(e)
	m.mu.Unlock()
	if m.testBeforeSupersede != nil {
		m.testBeforeSupersede(e)
	}
	// Supersession before the eviction closes anything (matter.js
	// SessionManager.ts:750-769 at b25ed960, #sessionEstablished).
	m.fireOnSessionSuperseded(superseded, e)
	m.establishMu.Unlock()
	closeStaleEntries(excess)
	m.fireOnSessionClose(excess)
	return true
}

// finishClose is the teardown of an entry [Manager.Close] took out of the
// table, outside the lock.
func (m *Manager) finishClose(entry *Entry) {
	// A pre-allocated id whose handshake never reached key derivation has
	// no Session under it; dropping the reservation is the whole teardown.
	if entry.Session != nil {
		entry.Session.Close()
	}
	m.fireOnSessionClose([]*Entry{entry})
	m.fireReannounceIfPeerGone([]*Entry{entry})
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
// sessions "to prevent re-announces when removing sessions" (at b25ed960,
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
		m.removeLocked(id, e)
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
			m.removeLocked(id, entry)
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
			m.removeLocked(id, entry)
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
			m.removeLocked(id, entry)
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
// answering is closed with [Manager.ClosePeerLostBefore] instead.
//
// Deprecated: nothing in the module calls it — the KeepSubscriptions=false
// teardown it was written for closes the peer's subscriptions, not its
// sessions (subscription.Manager.ClosePeer). There is no replacement
// that sends the farewell; [Manager.Close] on each id
// [Manager.SessionIDsForPeer] returns closes the established sessions
// without one — a session still establishing is not among those ids.
// Removable in v0.25.0 (review II, R2-P2 design note 4).
func (m *Manager) ClosePeer(fabricIndex uint8, peerNodeID uint64) int {
	m.mu.Lock()
	victims := make([]*Entry, 0)
	for id, entry := range m.sessions {
		if entry.Session == nil {
			// Pre-allocated id placeholder — the handshake that owns
			// it has not reached OpenFromSigmaWithStake yet. Leaving the
			// placeholder in place is harmless: the placeholder has
			// no key material, so an inbound packet for it cannot
			// authenticate.
			continue
		}
		if entry.FabricIndex() == fabricIndex && entry.Session.PeerNodeID() == peerNodeID {
			victims = append(victims, entry)
			m.removeLocked(id, entry)
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

// ClosePeerLostBefore force-closes every live session with the
// (fabricIndex, peerNodeID) pair that registered before registeredBefore —
// a value of [Manager.RegistrationCount] read when the exchange that
// observed the loss began. The exchange's own session is among them; a
// session that registered since, an establishing one included, carries the
// peer's data after the loss and is kept, whatever its creation time. No
// CloseSession goes on the wire: the peer is lost, so each session is
// marked peer-lost and force-closed. Returns the number of sessions closed.
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:742-748 at b25ed960
// (handlePeerLoss with PeerLossContext.registeredBefore),
// packages/protocol/src/session/SessionManager.ts:812-838 at b25ed960
// (#handlePeerLoss: a session not registered before the cutoff is skipped),
// packages/protocol/src/session/SessionManager.ts:1295-1298 at b25ed960
// (isRegisteredBefore: own position < registeredBefore),
// packages/protocol/src/session/NodeSession.ts:412-415 at b25ed960
// (handlePeerLoss: #isPeerLost, then initiateForceClose).
func (m *Manager) ClosePeerLostBefore(fabricIndex uint8, peerNodeID, registeredBefore uint64) int {
	return m.closePeerLost(fabricIndex, peerNodeID, func(entry *Entry) bool {
		return entry.registration >= registeredBefore
	})
}

// ClosePeerLost force-closes every live session with the
// (fabricIndex, peerNodeID) pair that was created before asOf, as
// [Manager.ClosePeerLostBefore] closes them; a session created in the same
// instant as asOf counts as newer and is kept. A zero asOf closes them all.
// asOf is compared with the session's wall-clock creation time
// ([Entry.CreatedAt]). Returns the number of sessions closed.
//
// Deprecated: two sessions may have the same creation time, so the time
// does not say which session is newer; use [Manager.ClosePeerLostBefore],
// which cuts by registration order. Removable in v0.26.0.
// Mirrors matter.js packages/protocol/src/peer/PeerLossContext.ts:42-48 at b25ed960
// (asOf, deprecated for registeredBefore),
// applied at packages/protocol/src/session/SessionManager.ts:832-834 at b25ed960.
func (m *Manager) ClosePeerLost(fabricIndex uint8, peerNodeID uint64, asOf time.Time) int {
	return m.closePeerLost(fabricIndex, peerNodeID, func(entry *Entry) bool {
		return !asOf.IsZero() && !entry.CreatedAt().Before(asOf)
	})
}

// ClosePeerLostSince is [Manager.ClosePeerLost] with the cutoff on the
// clock's monotonic reading ([clock.MonotonicOf], [Entry.CreatedAtMonotonic]):
// every live session of the peer created before sinceMono is force-closed,
// one created at or after it is kept, so a wall-clock step between the
// session's creation and the loss neither spares nor condemns it. Returns
// the number of sessions closed.
//
// Deprecated: two sessions may have the same creation time, so the time
// does not say which session is newer; use [Manager.ClosePeerLostBefore],
// which cuts by registration order. Removable in v0.26.0.
// Mirrors matter.js packages/protocol/src/peer/PeerLossContext.ts:42-48 at b25ed960
// (asOf on the clock of Time.nowUs, deprecated for registeredBefore),
// applied at packages/protocol/src/session/SessionManager.ts:832-834 at b25ed960.
func (m *Manager) ClosePeerLostSince(fabricIndex uint8, peerNodeID uint64, sinceMono time.Duration) int {
	return m.closePeerLost(fabricIndex, peerNodeID, func(entry *Entry) bool {
		return entry.CreatedAtMonotonic() >= sinceMono
	})
}

// closePeerLost is the sweep behind ClosePeerLostBefore, ClosePeerLost and
// ClosePeerLostSince: keep reports a session of the peer that survives the
// sweep. keep runs under m.mu.
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
		m.removeLocked(id, entry)
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
// Mirrors matter.js packages/protocol/src/session/SessionManager.ts:204-206,
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
// packages/protocol/src/session/SessionManager.ts:779-808,
// #evictExcessSessionsFor, and #leastRecentlyUsedSession (:600-608).
//
// Only a session the peer established runs the check: one we initiated
// evicts nothing, since the peer loss our own traffic reports sweeps
// those. Mirrors matter.js packages/protocol/src/session/SessionManager.ts:764-768
// (#sessionEstablished runs the check only for a responder session).
//
// The check runs once added is established and only established sessions
// count and are evicted: a session still establishing becomes established
// or closes within its handshake. Mirrors matter.js
// packages/protocol/src/session/SessionManager.ts:771-810 at b25ed960
// (#evictExcessSessionsFor over #establishedSessions, :790, :793-796).
func (m *Manager) evictExcessPeerSessionsLocked(added *Entry) []*Entry {
	if added.isPase || added.isInitiator || added.establishing {
		return nil
	}
	fabricIndex, peerNodeID := added.FabricIndex(), added.Session.PeerNodeID()
	var claimants []*Entry
	for _, entry := range m.sessions {
		if entry == added || entry.Session == nil || entry.isInitiator || entry.establishing {
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
		m.removeLocked(entry.SessionID, entry)
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
		m.removeLocked(c.id, c.e)
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
// OpenFromSigmaWithStake. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:219
// `this.#sessions.saveResumptionRecord(cx.resumptionRecord)`.
func (m *Manager) PersistResumption(ctx context.Context, fabricIndex uint8, peerNodeID uint64, resumptionID, sharedSecret []byte, peerCATs []uint32) error {
	return m.PersistResumptionRecord(ctx, store.ResumptionRecord{
		FabricIndex:  fabricIndex,
		PeerNodeID:   peerNodeID,
		ResumptionID: resumptionID,
		SharedSecret: sharedSecret,
		CASEAuthTags: peerCATs,
	})
}

// PersistResumptionRecord stores rec as [Manager.PersistResumption] does,
// with the peer's session parameters of the session the record resumes
// (rec.SessionParameters, zero = absent): the one record per peer a CASE
// client resumes from too (client/handshake.StoreResumption). matter.js's
// responder saves them with the record (CaseServer.ts:339 at db021da0,
// `sessionParameters: secureSession.parameters`).
func (m *Manager) PersistResumptionRecord(ctx context.Context, rec store.ResumptionRecord) error {
	return m.store.UpsertResumption(ctx, rec)
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
// packages/protocol/src/session/SessionManager.ts:581-599 at db021da0
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

// SessionIDsForPeer returns the ids of the live established sessions
// currently bound to (fabricIndex, peerNodeID), ordered by id. Staked ids
// are excluded: they carry no peer; so are sessions still establishing,
// which a picker does not hand out (matter.js maybeSessionFor reads the
// established set, packages/protocol/src/session/SessionManager.ts:683-700
// at b25ed960).
//
// Opening a session does not close the peer's earlier ones: only a
// session the peer establishes beyond [MaxSessionsPerPeer] closes its
// least recently active ones ([Manager.evictExcessPeerSessionsLocked]),
// so the list can hold several ids, and the bridge picks among them
// (bridge/case_initiator.go, the fallback when the registry has no
// [Manager.NewestSessionForPeer]).
func (m *Manager) SessionIDsForPeer(fabricIndex uint8, peerNodeID uint64) []uint16 {
	m.mu.RLock()
	var out []uint16
	for _, e := range m.sessions {
		if e == nil || e.Session == nil || e.establishing {
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

// NewestSessionForPeer returns the id of the live session with
// (fabricIndex, peerNodeID) the peer sent on last, among those usable
// accepts (nil accepts every one); false when none qualifies. Ties go to
// the higher id. usable runs without the manager's lock held, so it may
// call back into the manager.
//
// usable is asked about the candidates one at a time, newest first, and
// not again once it answered true: the id returned is the one of that
// last call. A caller may so keep what usable found for the id it
// accepted (bridge livePeerLink does) without comparing ids.
//
// Mirrors matter.js packages/protocol/src/peer/Peer.ts:582-602 at db021da0
// (newestSession: the highest activeTimestamp among the sessions neither
// closing nor peer-lost, optionally of one channel type — here usable's
// part); activeTimestamp moves only when a message is received
// (packages/protocol/src/session/Session.ts:142-147), the stamp
// [Entry.MarkActiveRx] moves. A closed session is not in the table, so it
// is never a candidate, nor is a session still establishing: matter.js's
// pickers read the established set (maybeSessionFor,
// packages/protocol/src/session/SessionManager.ts:683-700 at b25ed960; the
// isEstablishing table, NodeSession.ts:214-217), so a request is not
// routed onto a session whose handshake has not completed.
func (m *Manager) NewestSessionForPeer(fabricIndex uint8, peerNodeID uint64, usable func(id uint16) bool) (uint16, bool) {
	type candidate struct {
		id   uint16
		mono time.Duration
	}
	var cands []candidate
	m.mu.RLock()
	for _, e := range m.sessions {
		if e == nil || e.Session == nil || e.establishing {
			continue
		}
		if e.FabricIndex() != fabricIndex || e.Session.PeerNodeID() != peerNodeID {
			continue
		}
		e.mu.Lock()
		mono := e.lastPeerActivityMono
		e.mu.Unlock()
		cands = append(cands, candidate{id: e.SessionID, mono: mono})
	}
	m.mu.RUnlock()
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].mono != cands[j].mono {
			return cands[i].mono > cands[j].mono
		}
		return cands[i].id > cands[j].id
	})
	for _, c := range cands {
		if usable == nil || usable(c.id) {
			return c.id, true
		}
	}
	return 0, false
}

// Sessions returns a snapshot of every open session, ordered by session
// id so repeated calls read consistently.
//
// It exists because the daemon tracks per-session liveness that nothing
// could see: the reaper consumed it internally and no operator surface
// could answer whether a controller was still connected. The snapshot is
// a copy — callers cannot reach the session keys through it.
//
// A session still establishing is not listed: matter.js's
// getActiveSessionInformation reads the established set
// (packages/protocol/src/session/SessionManager.ts:1145-1149 at b25ed960).
func (m *Manager) Sessions() []SessionInfo {
	m.mu.RLock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, e := range m.sessions {
		if e == nil || e.Session == nil || e.establishing {
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
