// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// UnsecuredSessionID is the session id of the unsecured session. Mirrors
// matter.js packages/protocol/src/session/UnsecuredSession.ts:18
// (UNICAST_UNSECURE_SESSION_ID) at 0ceb202d.
const UnsecuredSessionID uint16 = 0

// Session is one session of the controller with a peer: the unsecured
// session a PASE or CASE handshake runs on, or a secure session the
// handshake established. It is safe for concurrent use.
type Session struct {
	m    *Manager
	id   uint16
	peer uint16 // the peer's session id, stamped into outbound headers

	// Secure session state; secure is nil on the unsecured session.
	secure               *channel.Session
	reported             *sessionparams.Parameters
	cats                 []uint32
	attestationChallenge []byte
	suppressPeerLoss     bool
	resumed              bool
	fabricIndex          uint8
	createdAt            time.Time
	closing              chan struct{}
	closingOnce          sync.Once

	// Unsecured session state: the initiator's ephemeral node id — ours,
	// or the peer's on an inbound session — and the reception window of
	// the peer's counters (matter.js
	// MessageReceptionStateUnencryptedWithRollover, UnsecuredSession.ts:31).
	nodeID uint64
	window *mrp.Window
	// inbound marks the unsecured session a peer's initiating message
	// created (ExchangeManager.ts:261-264): nodeID is the peer's, and our
	// messages name it as their destination.
	inbound bool

	mu              sync.Mutex
	addr            *net.UDPAddr
	params          sessionparams.Parameters
	timestamp       time.Time
	activeTimestamp time.Time
	exchanges       map[*Exchange]struct{}
	closed          bool
	peerLost        bool
	deferredClose   bool
}

// ID is the local session id: [UnsecuredSessionID] for the unsecured
// session, the id the peer addresses its messages to otherwise.
func (s *Session) ID() uint16 { return s.id }

// PeerSessionID is the peer's session id for this session.
func (s *Session) PeerSessionID() uint16 { return s.peer }

// Addr is the peer's UDP address.
func (s *Session) Addr() *net.UDPAddr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// SetAddr moves the session to the peer's address addr: every later
// transmission on the session goes there, an exchange with an address
// override aside — matter.js `session.channel.networkAddress = address`,
// the in-place migration of PeerAddressMonitor.ts:243 and :170
// (packages/protocol/src/peer/, 0ceb202d). A nil addr is ignored.
func (s *Session) SetAddr(addr *net.UDPAddr) {
	if addr == nil {
		return
	}
	s.mu.Lock()
	s.addr = addr
	s.mu.Unlock()
}

// IsSecure reports whether the session is encrypted (PASE or CASE).
func (s *Session) IsSecure() bool { return s.secure != nil }

// NodeID is our node id on the session: the ephemeral initiator node id
// on an unsecured session we opened, the operational node id on a CASE
// session and 0 on a PASE session and on an unsecured session the peer
// opened.
func (s *Session) NodeID() uint64 {
	if s.secure != nil {
		return s.secure.LocalNodeID()
	}
	if s.inbound {
		return 0
	}
	return s.nodeID
}

// IsInbound reports whether the session is an unsecured session the peer
// opened with an initiating message — the session a CASE responder runs
// on (matter.js ExchangeManager.ts:261-264).
func (s *Session) IsInbound() bool { return s.inbound }

// PeerNodeID is the peer's operational node id: 0 on the unsecured and on
// a PASE session.
func (s *Session) PeerNodeID() uint64 {
	if s.secure != nil {
		return s.secure.PeerNodeID()
	}
	return 0
}

// Parameters is the peer's session parameters as the session uses them:
// the fallbacks overlaid by what the peer reported — matter.js
// Session.ts:204-227 (parameters).
func (s *Session) Parameters() sessionparams.Parameters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.params
}

// ReportedParameters is what the peer reported in the handshake, nil when
// it reported nothing — matter.js NodeSession reportedSessionParameters.
func (s *Session) ReportedParameters() *sessionparams.Parameters {
	if s.reported == nil {
		return nil
	}
	p := *s.reported
	return &p
}

// SetTimingParameters replaces the peer's MRP intervals with those p
// states, the fallback for each one it does not — matter.js
// Session.ts:249-254 (set timingParameters), which the handshakes call
// when the responder's parameters arrive (PaseClient.ts:163-165,
// CaseClient.ts:245-247).
func (s *Session) SetTimingParameters(p sessionparams.Parameters) {
	i := sessionparams.Parameters{
		SessionIdleInterval:    orU32(p.SessionIdleInterval, FallbackIdleInterval),
		SessionActiveInterval:  orU32(p.SessionActiveInterval, FallbackActiveInterval),
		SessionActiveThreshold: orU16(p.SessionActiveThreshold, FallbackActiveThreshold),
	}
	s.mu.Lock()
	s.params.SessionIdleInterval = i.SessionIdleInterval
	s.params.SessionActiveInterval = i.SessionActiveInterval
	s.params.SessionActiveThreshold = i.SessionActiveThreshold
	s.mu.Unlock()
}

// CaseAuthenticatedTags are the session's CASE Authenticated Tags (nil on
// PASE) — matter.js NodeSession caseAuthenticatedTags.
func (s *Session) CaseAuthenticatedTags() []uint32 { return append([]uint32(nil), s.cats...) }

// AttestationChallenge is the attestation challenge the session keys
// yielded (the third 16 bytes of the key schedule; NodeSession.ts:88).
func (s *Session) AttestationChallenge() []byte {
	return append([]byte(nil), s.attestationChallenge...)
}

// FabricIndex is our fabric index of a CASE session, 0 on the unsecured
// and on a PASE session — the fabric half of matter.js
// NodeSession.peerAddress (NodeSession.ts:408-413).
func (s *Session) FabricIndex() uint8 { return s.fabricIndex }

// Resumed reports that a CASE session came from Sigma2_Resume.
func (s *Session) Resumed() bool { return s.resumed }

// IsPeerActive reports whether the peer sent a message within its active
// threshold — matter.js Session.ts:149-151. The unsecured session of an
// initiator starts idle (UnsecuredSession.ts:30), a secure one active
// (NodeSession.ts:119).
func (s *Session) IsPeerActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isPeerActiveLocked(s.m.clock.Now())
}

func (s *Session) isPeerActiveLocked(now time.Time) bool {
	if s.activeTimestamp.IsZero() {
		return false
	}
	return now.Sub(s.activeTimestamp) < IntervalsOf(s.params).ActiveThreshold
}

// notifyActivity records traffic: every message sent or received updates
// the session's timestamp, a received one also its active timestamp —
// matter.js Session.ts:141-147.
func (s *Session) notifyActivity(received bool) {
	now := s.m.clock.Now()
	s.mu.Lock()
	s.timestamp = now
	if received {
		s.activeTimestamp = now
	}
	s.mu.Unlock()
}

// lastTimestamp is the time of the session's last traffic.
func (s *Session) lastTimestamp() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.timestamp
}

// ActiveTimestamp is the time of the last message received from the peer
// on the session — matter.js Session.activeTimestamp (session/Session.ts:61-65,
// updated by notifyActivity :141-147), the order of Peer.newestSession
// (peer/Peer.ts:581-601). A secure session starts at its creation time
// (Session.ts:119-121 with NodeSession.ts:119); the unsecured session is
// zero until the peer's first message.
func (s *Session) ActiveTimestamp() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTimestamp
}

// timing is the peer's intervals and whether the peer is active now.
func (s *Session) timing() (Intervals, bool) {
	now := s.m.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	return IntervalsOf(s.params), s.isPeerActiveLocked(now)
}

// IsClosed reports whether the session was closed.
func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// IsPeerLost reports whether the session was given up because the peer
// is gone or closed it — matter.js NodeSession.isPeerLost
// (NodeSession.ts:320-322). A lost session is closing: no CloseSession
// goes to the peer.
func (s *Session) IsPeerLost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peerLost
}

// Closing is closed once the session begins to close — [Session.Close],
// [Manager.CloseSession] or a peer loss — matter.js Session.closing
// (Session.ts:70, emitted at :299), which a peer watches to drop the
// session (Peer.ts:169-174).
func (s *Session) Closing() <-chan struct{} { return s.closing }

func (s *Session) markClosing() {
	s.closingOnce.Do(func() { close(s.closing) })
}

// HandlePeerLoss gives the session up because the peer is gone: it is
// marked lost, the manager's PeerLost hook is called, every exchange on it
// is aborted with cause, and it is closed without a CloseSession on the
// wire. Mirrors matter.js NodeSession.ts:311-314 (handlePeerLoss) and
// :349-352 with Session.ts:318-331 (initiateForceClose). It is a no-op on
// a session already closed or lost.
func (s *Session) HandlePeerLoss(cause error) { s.handlePeerLoss(cause, nil) }

// handlePeerLoss is HandlePeerLoss sparing current, the exchange that
// observed the loss: matter.js lets it finish (PeerLossContext.currentExchange,
// Session.ts:324-327) and closes the session once it has
// (NodeSession.ts:354-362, deferredClose).
func (s *Session) handlePeerLoss(cause error, current *Exchange) {
	s.mu.Lock()
	if s.closed || s.peerLost {
		s.mu.Unlock()
		return
	}
	s.peerLost = true
	_, spared := s.exchanges[current]
	s.deferredClose = spared && current != nil
	s.mu.Unlock()
	s.markClosing()
	s.m.reportPeerLost(s, cause)
	for _, x := range s.liveExchanges() {
		if x != current {
			x.abort(cause)
		}
	}
	if !spared || current == nil {
		s.Close()
		return
	}
	s.closeIfDeferred()
}

// closeIfDeferred closes a lost session whose last exchange ended.
func (s *Session) closeIfDeferred() {
	s.mu.Lock()
	ready := s.deferredClose && len(s.exchanges) == 0
	s.mu.Unlock()
	if ready {
		s.Close()
	}
}

func (s *Session) addExchange(x *Exchange) {
	s.mu.Lock()
	s.exchanges[x] = struct{}{}
	s.mu.Unlock()
}

func (s *Session) deleteExchange(x *Exchange) {
	s.mu.Lock()
	delete(s.exchanges, x)
	s.mu.Unlock()
}

// liveExchanges is the session's exchanges.
func (s *Session) liveExchanges() []*Exchange {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Exchange, 0, len(s.exchanges))
	for x := range s.exchanges {
		out = append(out, x)
	}
	return out
}

// Close drops the session without telling the peer: its exchanges are
// closed with [ErrSessionClosed], it leaves the manager's tables and its
// keys are dropped. [Manager.CloseSession] tells the peer first. It is
// idempotent.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.markClosing()
	for _, x := range s.liveExchanges() {
		x.abort(ErrSessionClosed)
	}
	s.m.forget(s)
	if s.secure != nil {
		s.secure.Close()
	}
}
