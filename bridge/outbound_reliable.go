// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// ErrAckWaitTimeout named a wait for an outbound message's acknowledgement
// that ended without it.
//
// Deprecated: no function of the module returns it any more — the
// tracker's wait for an acknowledgement, which had no caller outside its
// tests, was removed in review II. Removable in v0.25.0
// (owner decision 174).
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
	// mu guards pending and rtt; it is not held while Tick sends.
	mu      sync.Mutex
	pending map[outboundKey]*outboundEntry
	// unsecured holds the keys of session 0's entries in pending, in the
	// order they were tracked; [unsecuredEntryLimit] bounds it, and its
	// first key is the entry a full table evicts.
	unsecured []outboundKey
	// secureOrder holds, per secure session, the exchanges that have an
	// entry in pending, the one tracked least recently first;
	// [secureExchangeLimit] bounds it. secureByExchange is each such
	// exchange's one entry ([outboundReliableTracker.track]).
	secureOrder      map[uint16][]secureSlot
	secureByExchange map[secureSlot]outboundKey
	// abandoned remembers, for a secure session, the messages [Tick] gave
	// up on, each until the instant it may be forgotten: a caller that
	// learns a message's counter only after the send — the subscription
	// reporter, which stores the report's record then — can still tell an
	// abandoned message from an acknowledged one ([Abandoned]). An entry
	// is kept for as long again as its message was pending: the message's
	// own retransmission schedule bounds the window it covers.
	abandoned map[outboundKey]time.Time
	// rtt holds the recent first-try round-trips for diagnostics.
	rtt mrpRTTWindow
	// baseIntervalFor resolves the peer-appropriate MRP base interval
	// for the session (peer-advertised active/idle interval selected
	// by peer activity — matter.js MRP.ts:181 at db91886c). nil, or a
	// session the resolver does not know, falls back to the spec idle
	// default.
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
	// evicted, when set, is told of an unsecured-session entry that lost
	// its place to a newer one ([unsecuredEntryLimit]). The tracker reports
	// it itself, so no call site can track at a full table unnoticed. Set
	// once after construction; called without the mutex.
	evicted func(exchangeID uint16, counter uint32)
	// secureEvicted, when set, is told of a secure-session entry that lost
	// its place: to a newer message of its unnamed slot (superseded; only
	// [outboundReliableTracker.Track] and
	// [outboundReliableTracker.TrackWithMargin] track there), or to a newer
	// exchange of its session ([secureExchangeLimit]). Set once after
	// construction; called without the mutex.
	secureEvicted func(sessionID, exchangeID uint16, counter uint32, superseded bool)
	// freed holds, per named slot, the channel a sender waiting for the
	// slot's message to be acknowledged waits on; dropLocked closes it when
	// the slot's entry goes, by its acknowledgement or otherwise
	// ([outboundReliableTracker.waitExchangeFree]).
	freed map[secureSlot]chan struct{}
}

// secureSlot names one exchange of a secure session in the retransmit
// table: its id and this node's role in it, since exchange ids are the
// initiator's — an exchange this node opened and one the peer opened that
// share an id are two exchanges with a slot each (review II, phase 7,
// R2-P7-req-03). Messages tracked without a role
// ([outboundReliableTracker.TrackWithMargin]) share the unnamed slot of
// their id.
type secureSlot struct {
	sessionID  uint16
	exchangeID uint16
	role       exchangeRole
}

// outboundEntry captures one in-flight reliable datagram and the
// retransmit schedule. The exchangeID is recorded alongside the
// counter so [outboundReliableTracker.AbandonUnsecuredExchange] can drop
// every pending entry of a handshake in one shot — used when the handshake
// ends or moves past its replies. The sessionID feeds the per-peer MRP
// interval selection on every retransmit.
type outboundEntry struct {
	counter    uint32
	sessionID  uint16
	exchangeID uint16
	// initiator is, on the unsecured session, the node id of the
	// handshake's initiator ([HandshakeKey]): exchange ids are the
	// initiator's choice, so a handshake's entries are those of its
	// initiator and its exchange. Zero on a secure session.
	initiator  uint64
	datagram   []byte
	dest       net.Addr
	retries    int
	nextSendAt time.Time
	// trackedAt is when the entry was tracked, on the schedule's clock; an
	// unsecured-session entry is dropped [unsecuredEntryLifetime] after it.
	trackedAt time.Time
	// role is this node's role in the exchange, when the tracker call
	// named it ([outboundReliableTracker.TrackOnExchange],
	// [outboundReliableTracker.trackKickable]). A named role makes
	// the entry its exchange's one outstanding message: the exchange takes
	// no further message until it is acknowledged, and the receive path
	// refuses a message that does not acknowledge it
	// ([Bridge.refuseUnacknowledging]).
	role exchangeRole
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
	// installed is the secure session the reply's handshake installed
	// before the peer acknowledged the reply, zero for none
	// ([outboundReliableTracker.trackUnsecuredInstalled]).
	installed uint16
	// sentWall is the tracker's clock reading when the datagram first
	// left, used only to report the peer's round-trip in diagnostics. It is
	// deliberately NOT the injected `now` the retransmit schedule runs on:
	// that is a test seam and may be a fixed instant, which would make
	// every reported round-trip zero. Nothing in the MRP state machine
	// reads this field.
	sentWall time.Time
	// kick is what the liveness kick reads of the entry's exchange
	// ([outboundReliableTracker.kickForLiveness]); nil for an entry not
	// tracked as an exchange's message, which is never kicked. sentAt is
	// the instant of the message's last transmission on the schedule's
	// clock (zero until its first send went out), interval the wait armed
	// at it, and inFlight marks a transmission under way — the first until
	// [outboundReliableTracker.markSent], a retransmission until its send
	// returned.
	kick     *exchangeKick
	sentAt   time.Time
	interval time.Duration
	inFlight bool
}

// exchangeKick is what the liveness kick reads of the exchange a tracked
// message belongs to: whether its protocol is BDX, the acknowledgement the
// message carries, and whether the exchange ended.
type exchangeKick struct {
	bdx        bool
	hasAck     bool
	ackCounter uint32
	ended      func() bool
}

// bdxMaxRetransmissionInterval is the retransmission interval that still
// fits every transmission of a BDX message inside the 30 s a BDX peer waits
// for the next message before it abandons the transfer: the budget over
// the four waits between five transmissions, less 100 ms for the last one
// to arrive — 7.4 s.
// Mirrors matter.js packages/protocol/src/protocol/MessageExchange.ts:62-78 at db91886c
// (BDX_PEER_RESPONSE_BUDGET, BDX_RESPONSE_BUDGET_MARGIN, BDX_MAX_RETRANSMISSION_INTERVAL).
const bdxMaxRetransmissionInterval = 30*time.Second/mrp.MaxRetransmissions - 100*time.Millisecond

// capBDXWait caps d, the wait armed after a transmission, at
// [bdxMaxRetransmissionInterval] for a message of a BDX exchange — every
// wait, the first and each retransmission's, in either role, below the
// peer's idle interval too; any other message's wait is d.
// Mirrors matter.js packages/protocol/src/protocol/MessageExchange.ts:1296-1319 at db91886c
// (#backOffFor: the BDX cap is taken after the fixed backoff is added).
func capBDXWait(kick *exchangeKick, d time.Duration) time.Duration {
	if kick != nil && kick.bdx {
		return min(d, bdxMaxRetransmissionInterval)
	}
	return d
}

// newOutboundReliableTracker returns a fresh, empty tracker.
// baseIntervalFor may be nil — every send then uses the spec idle
// default as its base interval.
func newOutboundReliableTracker(baseIntervalFor func(sessionID uint16, now time.Time) time.Duration, clk clock.Clock) *outboundReliableTracker {
	return &outboundReliableTracker{
		clock:            clock.Or(clk),
		pending:          make(map[outboundKey]*outboundEntry),
		abandoned:        make(map[outboundKey]time.Time),
		secureOrder:      make(map[uint16][]secureSlot),
		secureByExchange: make(map[secureSlot]outboundKey),
		baseIntervalFor:  baseIntervalFor,
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
// UnsecuredSession.ts:36, "When we are the initiator we assume the node
// is in idle mode" — the responder counts its peer active), its timing
// parameters are replaced by the initiator's (PaseServer.ts:153-156,
// CaseServer.ts:120-123 `session.timingParameters =
// initiatorSessionParams`; Session.ts:261-266 stores them), every
// received message refreshes the active timestamp (Session.ts:142-148
// notifyActivity) and retransmissionIntervalOf picks activeInterval while
// the peer is active (MRP.ts:181, isPeerActive: Session.ts:150-152 at
// db021da0).
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

// TrackOnExchange is [outboundReliableTracker.Track] for a message of a
// secure session's exchange in which this node is the initiator
// (initiator) or the responder: it refuses the message, tracking nothing,
// with [errExchangeAwaitsAck] while an earlier message of the same
// exchange still waits for its acknowledgement — the caller then does not
// send it. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:701-705 at db021da0
// (a send while #sentMessageToAck is set throws "has not been acked yet,
// cannot send a new message"; review II, hs 4b). An exchange of the other
// role that shares the id is another exchange, with its own slot
// ([secureSlot]).
func (t *outboundReliableTracker) TrackOnExchange(counter uint32, sessionID, exchangeID uint16, initiator bool, datagram []byte, dest net.Addr, now time.Time) error {
	role := roleOf(initiator)
	return t.track(counter, sessionID, exchangeID, 0, datagram, dest, now, 0, nil, role, nil)
}

// trackKickable is [outboundReliableTracker.TrackOnExchange] for a message
// of an exchange ([initiatedSecureExchange.Send]), with margin added to the
// base interval of every (re)transmission
// ([outboundReliableTracker.TrackWithMargin]) and with what the liveness
// kick reads of the exchange (a nil kick tracks a message that is never
// kicked): the role keeps it out of the other role's slot of the same id,
// and its give-up is reported with it, so only the exchange it belongs to
// ends. It refuses with [errExchangeAwaitsAck], tracking nothing, while the
// exchange's earlier message waits for its acknowledgement; the send waits
// for that acknowledgement first
// ([outboundReliableTracker.waitExchangeFree]), so the refusal is left for
// a concurrent send. The entry counts its first transmission under way
// until [outboundReliableTracker.markSent].
func (t *outboundReliableTracker) trackKickable(counter uint32, sessionID, exchangeID uint16, initiator bool, datagram []byte, dest net.Addr, now time.Time, margin time.Duration, kick *exchangeKick) error {
	return t.track(counter, sessionID, exchangeID, 0, datagram, dest, now, margin, nil, roleOf(initiator), kick)
}

// markSent records the first transmission of the kickable message counter
// of sessionID as gone out at at, the instant its schedule was armed from:
// from now on a liveness kick may retransmit it. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:840-842 at db91886c
// (#lastTransmissionAt once the first send resolved).
func (t *outboundReliableTracker) markSent(sessionID uint16, counter uint32, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p := t.pending[outboundKey{sessionID, counter}]; p != nil && p.kick != nil && p.sentAt.IsZero() {
		p.sentAt, p.inFlight = at, false
	}
}

// kickForLiveness is the liveness kick on the exchange (sessionID,
// exchangeID) of this node's role: when the peer sent dupCounter again —
// a duplicate requiring an acknowledgement — and [mrp.ShouldKickForLiveness]
// holds for the exchange's outstanding message, that message is
// retransmitted now through send, its acknowledgement of the duplicate
// with it, and its schedule restarts from now as a retransmission's would.
// active and threshold are the peer's active interval and threshold. It
// reports whether it retransmitted; a send that failed counts too, as
// matter.js's kick has replaced the standalone acknowledgement once it
// started the retransmission.
// Mirrors matter.js packages/protocol/src/protocol/MessageExchange.ts:593-646 at db91886c
// (#kickRetransmissionForLiveness),
// packages/protocol/src/protocol/MessageExchange.ts:956-1040 at db91886c
// (#retransmitMessage: counted, stamped, re-armed after the send).
func (t *outboundReliableTracker) kickForLiveness(sessionID, exchangeID uint16, initiator bool, dupCounter uint32, now time.Time, active, threshold time.Duration, send func(net.Addr, []byte) error) bool {
	if t == nil || sessionID == 0 {
		return false
	}
	t.mu.Lock()
	key, ok := t.secureByExchange[secureSlot{sessionID, exchangeID, roleOf(initiator)}]
	p := t.pending[key]
	if !ok || p == nil || p.kick == nil {
		t.mu.Unlock()
		return false
	}
	k := mrp.LivenessKick{
		BDX:               p.kick.bdx,
		Closed:            p.kick.ended != nil && p.kick.ended(),
		Armed:             !p.inFlight,
		PendingAcks:       p.kick.hasAck,
		PendingAckCounter: p.kick.ackCounter,
		DuplicateCounter:  dupCounter,
		// matter.js's #isTransmissionBudgetSpent(retransmissions + 1)
		// against MAX_TRANSMISSIONS: the tracker gives up once
		// [mrp.MaxRetransmissions] retransmissions went out.
		NextBudgetSpent: p.retries >= mrp.MaxRetransmissions,
		Transmitted:     !p.sentAt.IsZero(),
		Elapsed:         now.Sub(p.sentAt),
		Interval:        p.interval,
		ActiveInterval:  active,
		ActiveThreshold: threshold,
		AdditionalDelay: p.margin,
	}
	if !mrp.ShouldKickForLiveness(k) {
		t.mu.Unlock()
		return false
	}
	p.retries++
	delay := capBDXWait(p.kick, mrp.BackoffDuration(t.entryBase(p.sessionID, p.peer, now)+p.margin, p.retries, rand.Float64)+time.Duration(t.fixedBackoff.Load()))
	p.nextSendAt, p.interval, p.sentAt, p.inFlight = now.Add(delay), delay, now, true
	dest, datagram := p.dest, p.datagram
	t.mu.Unlock()
	_ = send(dest, datagram)
	t.settled(key, p)
	return true
}

// settled ends the transmission of p under way, if p is still the entry
// at key.
func (t *outboundReliableTracker) settled(key outboundKey, p *outboundEntry) {
	t.mu.Lock()
	if t.pending[key] == p {
		p.inFlight = false
	}
	t.mu.Unlock()
}

// roleOf is this node's exchange role: the initiator, or the responder.
func roleOf(initiator bool) exchangeRole {
	if initiator {
		return exchangeInitiator
	}
	return exchangeResponder
}

// exchangeRole is this node's role in the exchange of a tracked secure
// message, when its tracker call named it ([TrackOnExchange],
// [outboundReliableTracker.trackKickable]).
type exchangeRole uint8

const (
	exchangeRoleUnnamed exchangeRole = iota
	exchangeInitiator
	exchangeResponder
)

// errExchangeAwaitsAck is [outboundReliableTracker.TrackOnExchange]'s
// refusal: the exchange has a message that waits for its acknowledgement.
var errExchangeAwaitsAck = errors.New("bridge: outbound retransmit: the exchange's earlier message is not acknowledged yet")

// TrackWithMargin is [outboundReliableTracker.Track] with margin added to
// the base interval of every (re)transmission before the backoff
// amplifies it: matter.js MessageExchange #sendAdditionalDelay
// (MessageExchange.ts:1259-1262 at db021da0) passed as
// retransmissionIntervalOf's additionalDelay (MRP.ts, `baseInterval =
// (isPeerActive ? activeInterval : idleInterval) + additionalDelay`).
func (t *outboundReliableTracker) TrackWithMargin(counter uint32, sessionID, exchangeID uint16, datagram []byte, dest net.Addr, now time.Time, margin time.Duration) {
	_ = t.track(counter, sessionID, exchangeID, 0, datagram, dest, now, margin, nil, exchangeRoleUnnamed, nil)
}

// trackUnsecured tracks a reliable message on session 0: a responder's
// reply with the initiator's timing (peer set, its base interval then comes
// from peer, not from the session resolver, which knows no session 0), a
// message of a handshake this node opened on the idle default (peer nil).
//
// It always tracks. At a full table the entry tracked first loses its place
// ([unsecuredEntryLimit]) and the tracker reports it through its evicted
// hook.
func (t *outboundReliableTracker) trackUnsecured(counter uint32, key HandshakeKey, datagram []byte, dest net.Addr, now time.Time, peer *handshakePeerTiming) {
	t.trackUnsecuredInstalled(counter, key, datagram, dest, now, peer, 0)
}

// trackUnsecuredInstalled is [outboundReliableTracker.trackUnsecured] for a
// reply whose handshake installed the secure session installed before the
// peer acknowledged the reply (a Sigma2_Resume): [Tick] reports it with the
// entry's give-up, and the bridge closes it
// ([Bridge.closeUnacknowledgedResume]). Zero is none.
func (t *outboundReliableTracker) trackUnsecuredInstalled(counter uint32, key HandshakeKey, datagram []byte, dest net.Addr, now time.Time, peer *handshakePeerTiming, installed uint16) {
	_ = t.track(counter, 0, key.ExchangeID, key.InitiatorNodeID, datagram, dest, now, 0, peer, exchangeRoleUnnamed, nil)
	if installed == 0 {
		return
	}
	t.mu.Lock()
	if e := t.pending[outboundKey{0, counter}]; e != nil {
		e.installed = installed
	}
	t.mu.Unlock()
}

// forgetInstalled drops sessionID from every unsecured entry that names it
// as installed: the session closed, and its id may be given to another
// before the entry is given up ([Bridge.purgeSessionState]).
func (t *outboundReliableTracker) forgetInstalled(sessionID uint16) {
	if t == nil || sessionID == 0 {
		return
	}
	t.mu.Lock()
	for _, e := range t.pending {
		if e.installed == sessionID {
			e.installed = 0
		}
	}
	t.mu.Unlock()
}

// unsecuredEntryLimit is how many reliable messages the unsecured session
// holds for retransmission — the part of the table a peer fills without
// having authenticated: every PBKDFParamResponse, Pake2, Sigma2 and
// StatusReport of a handshake is one, paced by the intervals that peer
// stated, which neither reference clamps (matter.js
// packages/protocol/src/session/SessionIntervals.ts:52-53 at db91886c; chip
// src/protocols/secure_channel/PairingSession.cpp:175-189 at 6170af84).
//
// The rule is this module's own (review II, owner decision 23): a message
// on the unsecured session is always sent and always tracked, and when the
// table is full the entry tracked first loses its place. That entry's
// datagram went out when it was tracked; it loses its retransmissions and
// nothing else.
//
// What a flood then costs: retransmissions. A peer that sends
// unsecuredEntryLimit handshake messages between a legitimate handshake's
// reply and that reply's acknowledgement evicts it; the handshake fails
// only if the reply's datagram is lost as well — the initiator's
// retransmission of its request is acknowledged as a duplicate and not
// answered a second time ([Bridge.dispatch]) — and then at the initiator's
// own timeout. What a flood cannot do: keep a reply from being sent, or
// make the table grow.
//
// Why 16: under eviction the size has to cover the handshakes of honest
// peers that wait for an acknowledgement at the same moment — one PASE
// (the single-active slot, [Bridge.claimPase]) and the CASE
// handshakes of the controllers that reconnect together — and nothing
// more: a flood evicts at any size a device would choose, so a larger
// table buys memory, not protection. An entry is one datagram and its
// schedule.
//
// Neither reference has the rule. matter.js has no table: the message
// awaiting its acknowledgement and its timer are fields of the exchange
// (packages/protocol/src/protocol/MessageExchange.ts:285 and :293 at
// db021da0). connectedhomeip has a table for all sessions whose default
// size happens to be the same number (src/messaging/ReliableMessageMgr.h:267,
// src/lib/core/CHIPConfig.h:303-305 at 6170af84) and does not send a
// message that does not fit (src/messaging/ReliableMessageMgr.cpp:254-263,
// src/messaging/ExchangeMessageDispatch.cpp:63-64) — a bound it enforces
// only where its pools are static, which Linux and Darwin are not
// (src/lib/support/Pool.h:619-626,
// src/platform/Linux/SystemPlatformConfig.h:41). Refusing the newest
// message is what this module did first and gave up: any node on the link
// could then stop every handshake with sixteen Sigma1 a minute (review II,
// R2-P0-rv-A01).
const unsecuredEntryLimit = 16

// unsecuredEntryLifetime is how long a message on the unsecured session may
// wait for its acknowledgement. An entry normally ends sooner — with its
// acknowledgement, or when its handshake moves past it or is ended from outside
// ([outboundReliableTracker.AbandonUnsecuredExchange]) — but three kinds
// are bounded by no handshake timeout: the StatusReport that ends a
// handshake, which is retransmitted after the handshake is over and paced
// by the intervals the peer stated (clamped to an hour,
// [sessionparams.MaxUnauthenticatedInterval], where they are adopted —
// [handshakePeerTimingOf]); a CASE reply of a host that runs no reaper
// ([PerExchangeCaseProvider.StartReaper]); and a PASE reply whose slot
// nobody takes over, since [pasePairingTimeout] is looked at by the next
// PBKDFParamRequest only. The value is [pasePairingTimeout]'s — matter.js's
// PASE_PAIRING_TIMEOUT (packages/protocol/src/session/pase/PaseServer.ts:33
// at db021da0) — applied to every message of the unsecured session; the
// rule itself is this module's own.
const unsecuredEntryLifetime = 60 * time.Second

// secureExchangeLimit bounds the exchanges of one secure session that have
// a message waiting for its acknowledgement, and each exchange has one
// such message: a message tracked on an exchange that holds one takes its
// place, and a message of a new exchange past the limit takes the place of
// the exchange tracked least recently. The message that loses its place
// loses its retransmissions, nothing else — the rule session 0 has
// ([unsecuredEntryLimit]). Without it a peer that holds a session and
// never acknowledges adds an entry per request, each paced by the
// intervals it stated (up to a uint32 of milliseconds), for as long as the
// session lives, in memory of the whole node (review II, R2-P6-bound-0303).
//
// One message awaiting its acknowledgement per exchange is matter.js's rule,
// and matter.js refuses the new message (MessageExchange.ts:701-705 at
// db021da0). The bridge's interaction replies and reports are tracked
// through [outboundReliableTracker.TrackOnExchange], which refuses as
// matter.js does (hs 4b), and a BDX transfer's messages through
// [outboundReliableTracker.trackKickable], whose send first waits
// for the acknowledgement (review II, phase 7, decision 276); only a
// message tracked through [Track] or [TrackWithMargin] — a test's — still
// takes the older message's place of its unnamed slot, which then loses
// its retransmissions.
//
// The number is matter.js's: at most 30 non-closed exchanges per secure
// session, the least recently active closed beyond it
// (packages/protocol/src/protocol/ExchangeManager.ts:63, :578-595 at
// db021da0), the bound [maxExchangesPerSession] keeps on the bridge's
// exchange tables too.
const secureExchangeLimit = 30

// errUnsecuredEntryExpired is [outboundReliableTracker.Tick]'s report
// of an unsecured-session entry dropped after
// [unsecuredEntryLifetime].
var errUnsecuredEntryExpired = errors.New("bridge: outbound retransmit: the handshake's time is over")

// secureEviction is one secure-session entry that lost its place, reported
// after the tracker's mutex is released.
type secureEviction struct {
	key        outboundKey
	exchangeID uint16
	superseded bool
}

// evictSecureLocked drops the entry at key, a secure session's. Caller
// holds t.mu.
func (t *outboundReliableTracker) evictSecureLocked(key outboundKey, superseded bool) (secureEviction, bool) {
	e, ok := t.pending[key]
	if !ok {
		return secureEviction{}, false
	}
	ev := secureEviction{key: key, exchangeID: e.exchangeID, superseded: superseded}
	t.dropLocked(key)
	return ev, true
}

// placeSecureLocked makes room for the entry key of exchangeID on a secure
// session ([secureExchangeLimit]) and records it; it returns the entries
// that lost their place. Caller holds t.mu, and key is not in pending.
func (t *outboundReliableTracker) placeSecureLocked(key outboundKey, exchangeID uint16, role exchangeRole) []secureEviction {
	var out []secureEviction
	slot := secureSlot{key.sessionID, exchangeID, role}
	if prev, ok := t.secureByExchange[slot]; ok {
		if ev, ok := t.evictSecureLocked(prev, true); ok {
			out = append(out, ev)
		}
	}
	for len(t.secureOrder[key.sessionID]) >= secureExchangeLimit {
		first := t.secureOrder[key.sessionID][0]
		prev, ok := t.secureByExchange[first]
		if !ok {
			// Not reachable: every exchange in the order has its entry.
			t.secureOrder[key.sessionID] = t.secureOrder[key.sessionID][1:]
			continue
		}
		if ev, ok := t.evictSecureLocked(prev, false); ok {
			out = append(out, ev)
		}
	}
	t.secureOrder[key.sessionID] = append(t.secureOrder[key.sessionID], slot)
	t.secureByExchange[slot] = key
	return out
}

// reportSecureEvictions tells the hook of the entries that lost their
// place. Called without t.mu.
func (t *outboundReliableTracker) reportSecureEvictions(evs []secureEviction) {
	for _, ev := range evs {
		if t.secureEvicted != nil {
			t.secureEvicted(ev.key.sessionID, ev.exchangeID, ev.key.counter, ev.superseded)
		}
	}
}

func (t *outboundReliableTracker) track(counter uint32, sessionID, exchangeID uint16, initiator uint64, datagram []byte, dest net.Addr, now time.Time, margin time.Duration, peer *handshakePeerTiming, role exchangeRole, kick *exchangeKick) error {
	cp := make([]byte, len(datagram))
	copy(cp, datagram)
	// The initial wait before the first retransmit uses the full spec
	// formula at transmission 0 — matter.js MRP.ts:181 (at db91886c)
	// evaluates the peer-appropriate base interval for every
	// transmission including the first.
	delay := capBDXWait(kick, mrp.BackoffDuration(t.entryBase(sessionID, peer, now)+margin, 0, rand.Float64)+time.Duration(t.fixedBackoff.Load()))
	key := outboundKey{sessionID, counter}
	var (
		oldest     *outboundEntry
		secureGone []secureEviction
	)
	t.mu.Lock()
	if sessionID != 0 && role != exchangeRoleUnnamed {
		if prev, ok := t.secureByExchange[secureSlot{sessionID, exchangeID, role}]; ok && prev != key {
			if e := t.pending[prev]; e != nil {
				t.mu.Unlock()
				return errExchangeAwaitsAck
			}
		}
	}
	// A key tracked again takes a fresh place.
	t.dropLocked(key)
	delete(t.abandoned, key)
	if sessionID != 0 {
		secureGone = t.placeSecureLocked(key, exchangeID, role)
	}
	if sessionID == 0 {
		if len(t.unsecured) >= unsecuredEntryLimit {
			// The table is full: the entry tracked first loses its
			// place, never the new one ([unsecuredEntryLimit]).
			first := t.unsecured[0]
			oldest = t.pending[first]
			t.dropLocked(first)
		}
		t.unsecured = append(t.unsecured, key)
	}
	t.pending[key] = &outboundEntry{
		counter:    counter,
		sessionID:  sessionID,
		exchangeID: exchangeID,
		initiator:  initiator,
		datagram:   cp,
		dest:       dest,
		nextSendAt: now.Add(delay),
		trackedAt:  now,
		margin:     margin,
		peer:       peer,
		sentWall:   t.clock.Now(),
		role:       role,
		kick:       kick,
		interval:   delay,
		inFlight:   kick != nil,
	}
	t.mu.Unlock()
	if oldest != nil && t.evicted != nil {
		t.evicted(oldest.exchangeID, oldest.counter)
	}
	t.reportSecureEvictions(secureGone)
	if t.wake != nil {
		t.wake() // after the unlock: the entry is in pending first
	}
	return nil
}

// dropLocked removes the entry at key and its place in the unsecured
// session's order; every removal from pending goes through it. Caller
// holds t.mu.
func (t *outboundReliableTracker) dropLocked(key outboundKey) {
	e, ok := t.pending[key]
	if !ok {
		return
	}
	delete(t.pending, key)
	if key.sessionID == 0 {
		if i := slices.Index(t.unsecured, key); i >= 0 {
			t.unsecured = slices.Delete(t.unsecured, i, i+1)
		}
		return
	}
	slot := secureSlot{key.sessionID, e.exchangeID, e.role}
	if t.secureByExchange[slot] != key {
		return
	}
	delete(t.secureByExchange, slot)
	if ch, ok := t.freed[slot]; ok {
		close(ch)
		delete(t.freed, slot)
	}
	order := t.secureOrder[key.sessionID]
	if i := slices.Index(order, slot); i >= 0 {
		order = slices.Delete(order, i, i+1)
	}
	if len(order) == 0 {
		delete(t.secureOrder, key.sessionID)
	} else {
		t.secureOrder[key.sessionID] = order
	}
}

// holdsUnsecuredExchange reports whether a message of the handshake hk
// on the unsecured session still waits for its acknowledgement.
func (t *outboundReliableTracker) holdsUnsecuredExchange(hk HandshakeKey) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, key := range t.unsecured {
		if e, ok := t.pending[key]; ok && e.exchangeID == hk.ExchangeID && e.initiator == hk.InitiatorNodeID {
			return true
		}
	}
	return false
}

// errPreviousAckMissing is the receive path's refusal of a message that
// carries no acknowledgement while this node's message of its exchange
// waits for one ([Bridge.refuseUnacknowledging]).
var errPreviousAckMissing = errors.New("bridge: previous message ack is missing")

// errIncorrectAck is the receive path's refusal of a message whose
// acknowledgement names another counter than this node's outstanding
// message of its exchange ([Bridge.refuseUnacknowledging]).
var errIncorrectAck = errors.New("bridge: incorrect ack received")

// outstandingOn is the counters of this node's messages on the exchange a
// received message names that wait for their acknowledgement — none when
// the exchange holds none. On a secure session the exchange is the slot
// of the session, the exchange id and this node's role, the other one of
// the sender's ([secureSlot]); it holds at most one message, as every
// named slot does ([outboundReliableTracker.track]). On the unsecured
// session it is the handshake the message names ([HandshakeKey]), whose
// messages are tracked without that bound
// ([outboundReliableTracker.trackUnsecured]). In ordered dispatch it holds
// one there too — every reply answers a message that passed the receive
// rules and so acknowledged the waiting one — and the rule is matter.js's
// exactly; more than one only when two dispatches of a handshake overlap,
// where matter.js, with one outstanding message per exchange, has no state
// to compare with, and an acknowledgement of either is taken.
func (b *Bridge) outstandingOn(hdr *message.Header, proto message.ProtocolHeader) []uint32 {
	b.mu.RLock()
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if tracker == nil {
		return nil
	}
	if hdr.SessionID == 0 {
		return tracker.pendingOnHandshake(handshakeKeyOf(hdr, proto.ExchangeID))
	}
	if c, ok := tracker.pendingOnExchange(hdr.SessionID, proto.ExchangeID, roleOf(!proto.Initiator)); ok {
		return []uint32{c}
	}
	return nil
}

// pendingOnExchange is the counter of the message this node sent in role
// on the secure session's exchange that still waits for its
// acknowledgement — one tracked with its role named
// ([outboundReliableTracker.TrackOnExchange],
// [outboundReliableTracker.trackKickable]).
func (t *outboundReliableTracker) pendingOnExchange(sessionID, exchangeID uint16, role exchangeRole) (uint32, bool) {
	if t == nil || sessionID == 0 || role == exchangeRoleUnnamed {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key, ok := t.secureByExchange[secureSlot{sessionID, exchangeID, role}]
	if !ok || t.pending[key] == nil {
		return 0, false
	}
	return key.counter, true
}

// pendingOnSession reports whether a message this node sent on the secure
// session still waits for its acknowledgement — an entry of the session in
// the retransmit table, whatever its exchange or role.
func (t *outboundReliableTracker) pendingOnSession(sessionID uint16) bool {
	if t == nil || sessionID == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.secureOrder[sessionID]) > 0
}

// pendingOnHandshake is the counters of the messages of the handshake hk on
// the unsecured session that still wait for their acknowledgement, in the
// order they were tracked.
func (t *outboundReliableTracker) pendingOnHandshake(hk HandshakeKey) []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []uint32
	for _, key := range t.unsecured {
		if e, ok := t.pending[key]; ok && e.exchangeID == hk.ExchangeID && e.initiator == hk.InitiatorNodeID {
			out = append(out, key.counter)
		}
	}
	return out
}

// waitExchangeFree waits until the secure session's exchange holds no
// message this node sent in role that waits for its acknowledgement: the
// message was acknowledged, or dropped with its session or by an
// eviction. A message it waited on that was given up ends the wait with
// [mrp.ErrMaxRetransmissionsReached] — [Tick] records the give-up in
// abandoned in the lock section that drops the entry — and done closing
// ends it with errExchangeEnded, before or after a wake; ctx ending, with
// ctx's error. blocked, when set, is called once before the first wait —
// never when the exchange is free at once.
func (t *outboundReliableTracker) waitExchangeFree(ctx context.Context, sessionID, exchangeID uint16, role exchangeRole, done <-chan struct{}, blocked func()) error {
	slot := secureSlot{sessionID, exchangeID, role}
	told := false
	var waited []outboundKey
	for {
		t.mu.Lock()
		for _, k := range waited {
			if _, given := t.abandoned[k]; given {
				t.mu.Unlock()
				return mrp.ErrMaxRetransmissionsReached
			}
		}
		key, held := t.secureByExchange[slot]
		if !held || t.pending[key] == nil {
			t.mu.Unlock()
			select {
			case <-done:
				return errExchangeEnded
			default:
			}
			return nil
		}
		waited = append(waited, key)
		if t.freed == nil {
			t.freed = make(map[secureSlot]chan struct{})
		}
		ch, ok := t.freed[slot]
		if !ok {
			ch = make(chan struct{})
			t.freed[slot] = ch
		}
		t.mu.Unlock()
		if !told && blocked != nil {
			told = true
			blocked()
		}
		select {
		case <-ch:
		case <-done:
			return errExchangeEnded
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// errExchangeEnded is [outboundReliableTracker.waitExchangeFree]'s report
// that the exchange ended while its sender waited.
var errExchangeEnded = errors.New("bridge: the exchange ended while its message waited for an acknowledgement")

// Untrack drops a message Track registered whose send then failed: there is
// nothing on the wire to retransmit or to wait for.
func (t *outboundReliableTracker) Untrack(sessionID uint16, counter uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dropLocked(outboundKey{sessionID, counter})
}

// AbandonUnsecuredExchange drops every entry of the unsecured session of the
// handshake hk — its initiator and its exchange — that still waits for its
// acknowledgement. It returns the number of entries cleared.
//
// Called when the handshake moves past what it sent (the peer's next
// message arrived: a Sigma2 still retransmitted after Sigma3 is wasted at
// best, and Apple iOS drops it with "Dropping message without piggyback ack
// when we are waiting for an ack") — before the report of a completed
// handshake is sent, so that report stays tracked — and when the handshake
// is ended from outside while it waits for the peer: its responder was
// reaped or evicted, its handler was replaced or is gone. connectedhomeip
// aborts the exchange of a pairing session that is cleared while it still
// waits for a response, and clears its retransmission entry with it
// (src/messaging/ExchangeHolder.h:133-139, src/messaging/ExchangeContext.cpp:277-287
// and :241-246 at 6170af84, `Abort` → `DoClose(true)` →
// `ClearRetransTable(this)`). A handshake that fails on a message does not
// call it: what it sent goes on being retransmitted
// ([Bridge.sendHandshakeFailureReport], owner decision 173). Neither
// reference drops at its own timeout: connectedhomeip's response timeout
// clears the response-expected flag before the pairing session is cleared,
// so the exchange is closed, not aborted, and keeps its entry
// (ExchangeContext.cpp:470-475, :499-509, :261-270), and matter.js's PASE
// pairing timeout closes the exchange without a cause, which goes on
// retransmitting a message whose send still waits for its acknowledgement
// (packages/protocol/src/session/pase/PaseServer.ts:127-131, :207-221;
// packages/protocol/src/protocol/MessageExchange.ts:1165-1193, :937-943 at
// db021da0).
//
// Session 0 only, and that initiator's only: exchange ids are the
// initiator's choice, so an entry of a secure session whose peer happened
// to pick the same id stays, and so does one of another initiator's
// handshake on the same exchange id (matter.js keeps each initiator's
// exchanges in its own unsecured session, ExchangeManager.ts:308-312 and
// :401 at db021da0).
func (t *outboundReliableTracker) AbandonUnsecuredExchange(hk HandshakeKey) int {
	cleared, _ := t.abandonUnsecuredExchange(hk)
	return cleared
}

// abandonUnsecuredExchange is [outboundReliableTracker.AbandonUnsecuredExchange]
// that also returns the secure sessions the dropped entries' handshake
// installed before the peer acknowledged them
// ([outboundReliableTracker.trackUnsecuredInstalled]).
func (t *outboundReliableTracker) abandonUnsecuredExchange(hk HandshakeKey) (cleared int, installed []uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, key := range slices.Clone(t.unsecured) {
		e := t.pending[key]
		if e.exchangeID != hk.ExchangeID || e.initiator != hk.InitiatorNodeID {
			continue
		}
		if e.installed != 0 {
			installed = append(installed, e.installed)
		}
		t.dropLocked(key)
		cleared++
	}
	return cleared, installed
}

// DropSession drops every entry of the secure session sessionID — the
// session closed, so nothing it sent is retransmitted or acknowledged any
// more. It returns the number of entries dropped. Session 0 (the unsecured session) is left alone: its entries
// belong to handshakes, which end on their own clock. matter.js stops
// retransmitting when the send fails with SessionClosedError
// (MessageExchange.ts:1008-1014 at db021da0, the lines
// [outboundReliableTracker.Tick] cites); review II,
// R2-P2-sess-06.
func (t *outboundReliableTracker) DropSession(sessionID uint16) int {
	if t == nil || sessionID == 0 {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	dropped := 0
	for key := range t.pending {
		if key.sessionID != sessionID {
			continue
		}
		t.dropLocked(key)
		dropped++
	}
	for key := range t.abandoned {
		if key.sessionID == sessionID {
			delete(t.abandoned, key)
		}
	}
	return dropped
}

// Ack removes the pending entry for (sessionID, acked) whatever exchange it
// belongs to. Returns true when an entry was found, false on stray /
// duplicate ACK. A test helper: the receive path acknowledges inside the
// exchange the acknowledgement arrived on
// ([outboundReliableTracker.AckOnExchange]).
func (t *outboundReliableTracker) Ack(sessionID uint16, acked uint32) bool {
	return t.ack(outboundKey{sessionID, acked}, nil)
}

// AckOnExchange is the acknowledgement of acked received on the secure
// session's exchange exchangeID, in which this node has role: it clears
// the pending entry (sessionID, acked) only when that entry is a message of
// this exchange — its slot, or the unnamed slot of the id
// ([outboundReliableTracker.Track]) — and reports whether it did. An
// acknowledgement that names another exchange's message clears nothing
// there. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:521-542 at db021da0:
// the acknowledgement is evaluated inside the exchange only, against the
// one message it sent that still awaits its acknowledgement (review II,
// phase 7, decision 278).
func (t *outboundReliableTracker) AckOnExchange(sessionID, exchangeID uint16, role exchangeRole, acked uint32) bool {
	return t.ack(outboundKey{sessionID, acked}, func(e *outboundEntry) bool {
		return e.exchangeID == exchangeID && (e.role == role || e.role == exchangeRoleUnnamed)
	})
}

// ack is [outboundReliableTracker.Ack] for key; belongs, when set, must
// accept the pending entry, or the acknowledgement is not its and leaves
// it in place.
func (t *outboundReliableTracker) ack(key outboundKey, belongs func(*outboundEntry) bool) bool {
	t.mu.Lock()
	if e, ok := t.pending[key]; ok && belongs != nil && (e == nil || !belongs(e)) {
		t.mu.Unlock()
		return false
	}
	if _, ok := t.pending[key]; !ok {
		// A stray ACK: the duplicate ack of a retransmission after the
		// entry was cleared.
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
	t.mu.Unlock()
	return true
}

// AckUnsecured is [outboundReliableTracker.Ack] on the unsecured session:
// the acknowledgement of acked clears it only for the handshake hk it
// arrived on — its initiator and its exchange. Message counters on the
// unsecured session are one sequence for every handshake
// ([Bridge.nextUnsecuredCounter]), so the counter alone names no
// handshake. matter.js acts on an acknowledgement inside the exchange of
// the unsecured session the message names
// (packages/protocol/src/protocol/ExchangeManager.ts:306-312 and :398-401,
// MessageExchange.ts:521-537 at db021da0); an acknowledgement of another
// handshake's counter is stray there and is here.
func (t *outboundReliableTracker) AckUnsecured(hk HandshakeKey, acked uint32) bool {
	return t.ack(outboundKey{0, acked}, func(e *outboundEntry) bool {
		return e.exchangeID == hk.ExchangeID && e.initiator == hk.InitiatorNodeID
	})
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

// Abandoned reports whether [Tick] gave up on the message (sessionID,
// counter) of a secure session: it ran out of retransmissions without an
// acknowledgement. A message that is still pending, was acknowledged, was
// never tracked, or was abandoned so long ago that the tracker forgot it
// ([outboundReliableTracker.abandoned]) is not.
func (t *outboundReliableTracker) Abandoned(sessionID uint16, counter uint32) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.abandoned[outboundKey{sessionID, counter}]
	return ok
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
	// Initiator is the initiator node id of an unsecured-session entry's
	// handshake: with ExchangeID it names the handshake ([HandshakeKey]).
	Initiator uint64
	Counter   uint32
	// Role is this node's role in a secure entry's exchange, when its
	// tracker call named it: a give-up ends only the exchange of that role
	// (review II, phase 7, R2-P7-req-04).
	Role exchangeRole
	Err  error // nil = re-sent; mrp.ErrMaxRetransmissionsReached = abandoned
	// Installed is, for an unsecured entry given up or expired, the secure
	// session its handshake installed before the peer acknowledged it
	// ([outboundReliableTracker.trackUnsecuredInstalled]); zero for none.
	Installed uint16
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
// Mirrors matter.js MessageExchange.ts:941-942 and :1008-1014 at db021da0
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
	for key, forgetAt := range t.abandoned {
		if !now.Before(forgetAt) {
			delete(t.abandoned, key)
		}
	}
	for key, p := range t.pending {
		// A reply on the unsecured session whose handshake's time is over
		// is dropped, due or not: [unsecuredEntryLifetime].
		if key.sessionID == 0 && now.Sub(p.trackedAt) >= unsecuredEntryLifetime {
			t.dropLocked(key)
			out = append(out, outboundTickResult{SessionID: key.sessionID, ExchangeID: p.exchangeID, Initiator: p.initiator, Counter: key.counter, Err: errUnsecuredEntryExpired, Installed: p.installed})
			continue
		}
		if now.Before(p.nextSendAt) {
			continue
		}
		if p.retries >= mrp.MaxRetransmissions {
			t.dropLocked(key)
			if key.sessionID != 0 {
				t.abandoned[key] = now.Add(now.Sub(p.trackedAt))
			}
			out = append(out, outboundTickResult{SessionID: key.sessionID, ExchangeID: p.exchangeID, Initiator: p.initiator, Counter: key.counter, Role: p.role, Err: mrp.ErrMaxRetransmissionsReached, Installed: p.installed})
			continue
		}
		p.retries++
		// Full spec backoff per Matter §4.12.2.1: peer-appropriate
		// base interval (re-evaluated per retransmission — the peer
		// may have gone idle since the last attempt), margin,
		// exponential growth past the threshold, and jitter. Mirrors
		// matter.js MRP.ts:125-146 retransmissionIntervalOf.
		delay := capBDXWait(p.kick, mrp.BackoffDuration(t.entryBase(p.sessionID, p.peer, now)+p.margin, p.retries, rand.Float64)+time.Duration(t.fixedBackoff.Load()))
		p.nextSendAt = now.Add(delay)
		// The liveness kick's view: stamped before the send, the
		// transmission under way until it returned (MessageExchange.ts:1012,
		// :1029 at b25ed960).
		p.interval, p.sentAt, p.inFlight = delay, now, true
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
		err := send(r.dest, r.datagram)
		t.settled(r.key, r.p)
		if err != nil {
			out = append(out, outboundTickResult{SessionID: r.key.sessionID, ExchangeID: r.p.exchangeID, Counter: r.key.counter, Err: err})
			continue
		}
		out = append(out, outboundTickResult{SessionID: r.key.sessionID, Counter: r.key.counter})
	}
	return out
}
