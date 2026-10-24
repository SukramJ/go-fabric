// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// MaxConcurrentExchangesPerSession is the number of open exchanges one
// secure session keeps; opening one more closes the least recently active
// one. Mirrors matter.js packages/protocol/src/protocol/ExchangeManager.ts:54
// (MAXIMUM_CONCURRENT_OUTGOING_EXCHANGES_PER_SESSION = 30) and the
// eviction of :494-531 at 0ceb202d; the unsecured session is exempt
// (:511-514).
const MaxConcurrentExchangesPerSession = 30

// initiatedExchangeFlag sets an initiated exchange's table index apart
// from a received exchange with the same id — matter.js
// ExchangeManager.ts:147 (`exchangeId | 0x10000`) and :333-335.
const initiatedExchangeFlag uint32 = 0x10000

// sessionIDUpperBound is the largest local session id; ids cycle through
// 1..sessionIDUpperBound — matter.js session/SessionManager.ts:183
// (ID_SPACE_UPPER_BOUND = 0xffff), :552-557.
const sessionIDUpperBound uint16 = 0xFFFF

// Operational node id bounds an ephemeral initiator node id is drawn
// from — matter.js packages/types/src/datatype/NodeId.ts:45-52
// (randomOperationalNodeId) with OPERATIONAL_NODE_MIN / MAX.
const (
	operationalNodeMin uint64 = 0x0000_0000_0000_0001
	operationalNodeMax uint64 = 0xFFFF_FFEF_FFFF_FFFF
)

// Conn is the send side of the socket a [Manager] uses. A
// *transport/udp.Listener satisfies it; its Serve handler is
// [Manager.HandleDatagram].
type Conn interface {
	Send(dst *net.UDPAddr, payload []byte) error
}

// Config configures a [Manager].
type Config struct {
	// LocalParameters are our session parameters: sent in the handshakes
	// and the timing of our own retransmissions. Nil is
	// [DefaultLocalParameters].
	LocalParameters *sessionparams.Parameters
	// Clock is the time source; nil is [SystemClock].
	Clock Clock
	// Rand returns the MRP jitter draw in [0, 1); nil is math/rand/v2.Float64.
	Rand func() float64
	// Logger receives debug diagnostics; nil is slog.Default().
	Logger *slog.Logger
	// PeerLost is called when a send or a wait on an exchange of a secure
	// session fails in a way that shows the peer is gone — matter.js
	// MessageExchangeContext.peerLost (ExchangeManager.ts:556-579), fired
	// from MessageExchange.ts:613-621 and :822-832. Nil ignores it.
	PeerLost func(s *Session, cause error)
	// Retransmitted is called once per message the manager sends again
	// because its acknowledgement did not arrive in time — the hook a
	// metrics port counts mrp.retransmits with. It runs on the exchange's
	// timer goroutine and must not block. Nil ignores it.
	Retransmitted func()
	// Reliable marks the manager's carrier as reliable (BTP): its sessions
	// do not use MRP — no acknowledgement requested, none sent, no
	// retransmission — and a wait for the peer is the reliable channel's.
	// Mirrors matter.js packages/protocol/src/session/Session.ts:355-356
	// (`usesMrp = supportsMRP && !channel?.isReliable`) for a manager whose
	// one channel is a BleChannel (packages/protocol/src/ble/Ble.ts:38).
	Reliable bool
}

// Manager is the controller's exchange manager: one socket, the session
// table, the exchange table and the receive path. It is safe for
// concurrent use.
type Manager struct {
	conn     Conn
	clock    Clock
	rand     func() float64
	log      *slog.Logger
	local    sessionparams.Parameters
	peerLost func(s *Session, cause error)
	// retransmitted is [Config.Retransmitted].
	retransmitted func()
	reliable      bool

	// inflight counts the messages delivered to an exchange whose reader
	// has not finished with them (internal/inflight ExchangeDelivered):
	// the group the clock carries, nil outside a fabrictest site.
	inflight *inflight.Group

	// counter is the global unencrypted message counter every unsecured
	// session sends with (SessionManager.ts:265, :439).
	counter *mrp.Counter

	// work is the goroutines the manager starts — the protocol handler of
	// each inbound exchange and the close of an evicted exchange — added
	// under mu while the manager is open and waited for by
	// [Manager.Shutdown].
	work sync.WaitGroup

	// mu guards closed, the handler, session and exchange tables and the
	// id counters below.
	mu             sync.Mutex
	closed         bool
	handlers       map[uint16]ProtocolHandler
	sessions       map[uint16]*Session
	unsecured      map[uint64]*Session
	exchanges      map[uint32]*Exchange
	exchangeID     uint16
	nextSessionID  uint16
	sessionIDLimit uint16
}

// NewManager returns a manager that sends on conn.
func NewManager(conn Conn, cfg Config) (*Manager, error) {
	if conn == nil {
		return nil, errors.New("exchange: nil Conn")
	}
	counter, err := mrp.NewCounter()
	if err != nil {
		return nil, fmt.Errorf("exchange: message counter: %w", err)
	}
	var seed [4]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, fmt.Errorf("exchange: seed: %w", err)
	}
	m := &Manager{
		conn:          conn,
		clock:         cfg.Clock,
		rand:          cfg.Rand,
		log:           cfg.Logger,
		local:         DefaultLocalParameters(),
		peerLost:      cfg.PeerLost,
		retransmitted: cfg.Retransmitted,
		reliable:      cfg.Reliable,
		counter:       counter,
		sessions:      make(map[uint16]*Session),
		// unsecured is keyed by our ephemeral initiator node id
		// (SessionManager.ts:446-454).
		unsecured: make(map[uint64]*Session),
		exchanges: make(map[uint32]*Exchange),
		handlers:  make(map[uint16]ProtocolHandler),
		// The exchange counter and the session-id cursor start at a
		// random value (ExchangeManager.ts:729-734, SessionManager.ts:264).
		exchangeID:     binary.LittleEndian.Uint16(seed[0:2]),
		sessionIDLimit: sessionIDUpperBound,
	}
	m.nextSessionID = binary.LittleEndian.Uint16(seed[2:4])%sessionIDUpperBound + 1
	if cfg.LocalParameters != nil {
		m.local = *cfg.LocalParameters
	}
	if m.clock == nil {
		m.clock = SystemClock{}
	}
	m.inflight = inflight.Of(m.clock)
	if m.rand == nil {
		m.rand = mrand.Float64
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	return m, nil
}

// LocalParameters is our session parameters.
func (m *Manager) LocalParameters() sessionparams.Parameters { return m.local }

// Reliable reports whether the manager's carrier is reliable ([Config].Reliable).
func (m *Manager) Reliable() bool { return m.reliable }

// ProtocolHandler handles an exchange the peer opened: x is the new
// exchange, first its first message, already taken by x — acknowledged as
// x acknowledges any message, and not returned by [Exchange.Next], which
// waits for the message after it. It is matter.js
// ProtocolHandler.onNewExchange (packages/protocol/src/protocol/ProtocolHandler.ts:22-23
// at 0ceb202d). It runs on a goroutine of its own, one per exchange, so
// the receive path never waits for it; the exchange is closed when it
// returns, if the handler did not close it.
type ProtocolHandler func(x *Exchange, first *Message)

// SetProtocolHandler installs h for the exchanges the peer opens with
// protocol — matter.js ExchangeManager.addProtocolHandler, consulted for
// the first message of an exchange at ExchangeManager.ts:391-440. A nil h
// removes it. The Secure Channel protocol is special:
//
//   - on a secure session its handler stays the built-in one (the
//     StatusReport-only handler of SecureChannelProtocol.ts:30-88), h is
//     not consulted;
//   - on an unsecured session h receives a Sigma1 — matter.js
//     SecureChannelProtocol.ts:146-148 hands it to the CaseServer — and a
//     PBKDFParamRequest is refused with InvalidParam, the controller having
//     no PASE server (:129-137).
//
// Every other protocol's handler runs for the exchanges opened on a
// secure session only, as matter.js's InteractionServer and
// ClientSubscriptionHandler require (requiresSecureSession,
// ExchangeManager.ts:393-405). A message no handler takes is acknowledged
// when it asks for it and dropped (:441-473).
func (m *Manager) SetProtocolHandler(protocol uint16, h ProtocolHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h == nil {
		delete(m.handlers, protocol)
		return
	}
	m.handlers[protocol] = h
}

func (m *Manager) handler(protocol uint16) ProtocolHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handlers[protocol]
}

// NewUnsecuredSession opens the unsecured session of a handshake with the
// peer at addr. peer is what is known of the peer's session parameters
// (from its DNS-SD record), nil for none. Our side is the initiator: the
// session draws a random ephemeral node id, sends with the global
// unencrypted counter and counts the peer as idle until it answers —
// matter.js SessionManager.ts:418-458 (createUnsecuredSession) and
// UnsecuredSession.ts:26-34.
func (m *Manager) NewUnsecuredSession(addr *net.UDPAddr, peer *sessionparams.Parameters) (*Session, error) {
	if addr == nil {
		return nil, errors.New("exchange: nil peer address")
	}
	s := &Session{
		m:         m,
		addr:      addr,
		window:    mrp.NewWindow(),
		params:    PeerParameters(peer),
		exchanges: make(map[*Exchange]struct{}),
		closing:   make(chan struct{}),
	}
	s.timestamp = m.clock.Now()
	s.createdAt = s.timestamp
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrSessionClosed
	}
	for tries := 0; ; tries++ {
		id, err := randomOperationalNodeID()
		if err != nil {
			return nil, err
		}
		if _, taken := m.unsecured[id]; !taken {
			s.nodeID = id
			m.unsecured[id] = s
			return s, nil
		}
		if tries >= 4 {
			return nil, errors.New("exchange: unable to allocate a unique ephemeral node id")
		}
	}
}

// randomOperationalNodeID draws a node id in the operational range.
func randomOperationalNodeID() (uint64, error) {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("exchange: random node id: %w", err)
		}
		if id := binary.LittleEndian.Uint64(b[:]); id >= operationalNodeMin && id <= operationalNodeMax {
			return id, nil
		}
	}
}

// AllocateSessionID returns the next free local session id for a PASE or
// CASE session: the cursor's next id that no session holds, cycling
// through 1..0xFFFF; when every id is taken the least recently used
// session is closed (the peer is told) and its id reused. Mirrors matter.js
// SessionManager.ts:504-557 (getNextAvailableSessionId,
// findOldestInactiveSession, #takeSessionId).
func (m *Manager) AllocateSessionID(ctx context.Context) (uint16, error) {
	m.mu.Lock()
	for range int(m.sessionIDLimit) {
		id := m.takeSessionIDLocked()
		if _, taken := m.sessions[id]; !taken {
			m.mu.Unlock()
			return id, nil
		}
	}
	var oldest *Session
	for _, s := range m.sessions {
		if oldest == nil || s.lastTimestamp().Before(oldest.lastTimestamp()) {
			oldest = s
		}
	}
	m.mu.Unlock()
	if oldest == nil {
		return 0, ErrNoSessionID
	}
	m.CloseSession(ctx, oldest)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSessionID = oldest.id
	return m.takeSessionIDLocked(), nil
}

// takeSessionIDLocked returns the cursor's id and advances it.
func (m *Manager) takeSessionIDLocked() uint16 {
	id := m.nextSessionID
	if id >= m.sessionIDLimit {
		m.nextSessionID = 1
	} else {
		m.nextSessionID = id + 1
	}
	return id
}

// SecureSessionConfig describes a session a handshake established.
type SecureSessionConfig struct {
	// ID is the local session id ([Manager.AllocateSessionID]).
	ID uint16
	// Addr is the peer's address.
	Addr *net.UDPAddr
	// Channel carries the keys, the node ids and the peer's session id.
	Channel channel.Config
	// PeerParameters is the peer's parameters: the fallbacks overlaid by
	// what it reported (SessionParameters.ts:42-81).
	PeerParameters sessionparams.Parameters
	// ReportedParameters is what the peer reported, nil for nothing.
	ReportedParameters *sessionparams.Parameters
	// CaseAuthenticatedTags are the session's CATs (CASE only).
	CaseAuthenticatedTags []uint32
	// AttestationChallenge is the third 16 bytes of the session keys.
	AttestationChallenge []byte
	// SuppressPeerLoss keeps the session open when an exchange fails
	// instead of reporting the peer lost — matter.js NodeSession
	// suppressPeerLoss (NodeSession.ts:316-318), set for the PASE session
	// of a commissioning.
	SuppressPeerLoss bool
	// Resumed marks a session from Sigma2_Resume.
	Resumed bool
	// FabricIndex is our fabric index of a CASE session, 0 for PASE: with
	// the channel's peer node id it is the session's peer address
	// (NodeSession.ts:408-413), the key of [Manager.HandlePeerLoss].
	FabricIndex uint8
}

// NewSecureSession registers the secure session a handshake established.
// It counts the peer as active from the start (NodeSession.ts:119).
func (m *Manager) NewSecureSession(cfg SecureSessionConfig) (*Session, error) {
	if cfg.ID == UnsecuredSessionID {
		return nil, errors.New("exchange: secure session id 0")
	}
	if cfg.Addr == nil {
		return nil, errors.New("exchange: nil peer address")
	}
	ch, err := channel.New(cfg.Channel)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}
	now := m.clock.Now()
	s := &Session{
		m:                    m,
		id:                   cfg.ID,
		peer:                 cfg.Channel.PeerSessionID,
		addr:                 cfg.Addr,
		secure:               ch,
		cats:                 append([]uint32(nil), cfg.CaseAuthenticatedTags...),
		attestationChallenge: append([]byte(nil), cfg.AttestationChallenge...),
		suppressPeerLoss:     cfg.SuppressPeerLoss,
		resumed:              cfg.Resumed,
		fabricIndex:          cfg.FabricIndex,
		createdAt:            now,
		closing:              make(chan struct{}),
		params:               cfg.PeerParameters,
		timestamp:            now,
		activeTimestamp:      now,
		exchanges:            make(map[*Exchange]struct{}),
	}
	if cfg.ReportedParameters != nil {
		p := *cfg.ReportedParameters
		s.reported = &p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		ch.Close()
		return nil, ErrSessionClosed
	}
	if _, taken := m.sessions[cfg.ID]; taken {
		ch.Close()
		return nil, fmt.Errorf("exchange: session id %d is taken", cfg.ID)
	}
	m.sessions[cfg.ID] = s
	return s, nil
}

// Session returns the secure session with local id id, nil for none.
func (m *Manager) Session(id uint16) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// forget removes s from the session tables.
func (m *Manager) forget(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.secure == nil {
		if m.unsecured[s.nodeID] == s {
			delete(m.unsecured, s.nodeID)
		}
		return
	}
	if m.sessions[s.id] == s {
		delete(m.sessions, s.id)
	}
}

// CloseSession ends a session: a secure session whose peer is not lost
// (NodeSession.ts:389-395) first sends CloseSession
// on a fresh exchange — a StatusReport (Success, CloseSession) without an
// acknowledgement request — then is dropped. Mirrors matter.js
// ExchangeManager.ts:716-726 (#sendCloseSession) and
// SecureChannelMessenger.ts:193-195 (sendCloseSession, suppressAck). A
// failure to send is logged, never returned: the session is gone either
// way.
func (m *Manager) CloseSession(ctx context.Context, s *Session) {
	if s.IsClosed() {
		return
	}
	if s.secure != nil && !s.IsPeerLost() {
		if x, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{}); err == nil {
			report := StatusReport{
				GeneralCode:  mrp.SCStatusGeneralSuccess,
				ProtocolID:   uint32(mrp.SecureChannelProtocolID),
				ProtocolCode: statusCloseSession,
			}
			if err := x.Send(ctx, mrp.SCOpcodeStatusReport, report.Encode(), SendOptions{SuppressAck: true}); err != nil {
				m.log.Debug("matter.controller.close_session_failed", slog.Int("session_id", int(s.id)), slog.Any("err", err))
			}
			x.Close()
		}
	}
	s.Close()
}

// statusCloseSession is the Secure Channel protocol code CloseSession
// (Matter §4.10.1.3).
const statusCloseSession uint16 = 0x0003

// Close closes every exchange and session without telling the peers and
// waits for the goroutines the manager started: [Manager.Shutdown] without
// a bound.
func (m *Manager) Close() {
	m.closeAll()
	m.work.Wait()
}

// Shutdown closes every exchange and session without telling the peers,
// then waits, bounded by ctx, for the protocol handlers of inbound
// exchanges to return — closing their exchanges ends any wait on the
// peer, so a handler returns unless host code it calls blocks. It
// returns ctx's error when a handler still runs. It is idempotent; the
// manager accepts no new session and starts no handler afterwards.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.closeAll()
	idle := make(chan struct{})
	go func() {
		m.work.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("exchange: shutdown: %w", ctx.Err())
	}
}

// goWork runs fn on a goroutine [Manager.Shutdown] waits for, unless the
// manager is closed; it reports whether fn was started.
func (m *Manager) goWork(fn func()) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.work.Go(fn)
	return true
}

// closeAll marks the manager closed and closes every session.
func (m *Manager) closeAll() {
	m.mu.Lock()
	m.closed = true
	all := make([]*Session, 0, len(m.sessions)+len(m.unsecured))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	for _, s := range m.unsecured {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.Close()
	}
}

// Initiate opens an exchange on s for protocol — matter.js
// ExchangeManager.ts:145-156 (initiateExchangeForSession): the exchange id
// is the next value of a counter that starts at random and wraps at
// 0xFFFF (:729-743), and the exchange is indexed apart from received ones.
func (m *Manager) Initiate(s *Session, protocol uint16, opts Options) (*Exchange, error) {
	if s.IsClosed() {
		return nil, ErrSessionClosed
	}
	m.mu.Lock()
	m.exchangeID++
	id := m.exchangeID
	x := newExchange(m, s, id, protocol, true, opts)
	m.exchanges[x.index] = x
	m.mu.Unlock()
	s.addExchange(x)
	m.cleanupSessionExchanges(s)
	return x, nil
}

// removeExchange drops x from the table if the index still maps to it.
func (m *Manager) removeExchange(x *Exchange) {
	m.mu.Lock()
	if m.exchanges[x.index] == x {
		delete(m.exchanges, x.index)
	}
	m.mu.Unlock()
	x.session.deleteExchange(x)
	x.session.closeIfDeferred()
}

// cleanupSessionExchanges closes the least recently active exchange of s
// once it has more than [MaxConcurrentExchangesPerSession] open ones —
// matter.js ExchangeManager.ts:510-531.
func (m *Manager) cleanupSessionExchanges(s *Session) {
	if s.secure == nil {
		return
	}
	var open []*Exchange
	for _, x := range s.liveExchanges() {
		if !x.considerClosed() {
			open = append(open, x)
		}
	}
	if len(open) <= MaxConcurrentExchangesPerSession {
		return
	}
	lru := open[0]
	for _, x := range open[1:] {
		if x.lastActivity().Before(lru.lastActivity()) {
			lru = x
		}
	}
	m.log.Debug("matter.controller.exchange_evicted", slog.Int("session_id", int(s.id)), slog.Int("exchange_id", int(lru.id)))
	// On a goroutine of the manager: the caller is the receive path or an
	// Initiate. A closed manager closed the session's exchanges already.
	m.goWork(lru.Close)
}

// HandleDatagram processes one received datagram; it is the
// transport/udp Handler of the controller's socket. Mirrors matter.js
// ExchangeManager.ts:220-475 (#receiveMessage) for unicast messages: a
// message for an unknown session is dropped, the counter goes through
// the session's reception window (a duplicate still reaches its
// exchange, which acknowledges it), every decoded message marks the peer
// active, and a message no exchange of ours accepts is acknowledged when
// it asks for it and dropped otherwise.
func (m *Manager) HandleDatagram(buf []byte, src *net.UDPAddr) {
	hdr, hl, err := message.UnmarshalHeader(buf)
	if err != nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "header"), slog.Any("err", err))
		return
	}
	if hdr.SessionType != message.SessionUnsecured || hdr.Privacy {
		// No group sessions; a unicast message with the privacy flag is
		// invalid (ExchangeManager.ts:230-235).
		m.log.Debug("matter.controller.drop", slog.String("reason", "session type"))
		return
	}
	body := append([]byte(nil), buf[hl:]...)
	s, duplicate, inbound, ok := m.openSession(&hdr, &body, src)
	if !ok {
		return
	}
	if inbound {
		// An unsecured session created for this message does not outlive
		// it unless a handler took an exchange on it (ExchangeManager.ts:205-217).
		defer m.releaseInbound(s)
	}
	proto, pl, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "protocol header"), slog.Any("err", err))
		return
	}
	// Every decoded message proves the peer active, a duplicate too
	// (ExchangeManager.ts:290-292).
	s.notifyActivity(true)
	msg := &Message{
		Counter:     hdr.MessageCounter,
		ProtocolID:  proto.ProtocolID,
		Opcode:      proto.Opcode,
		ExchangeID:  proto.ExchangeID,
		Initiator:   proto.Initiator,
		RequiresAck: proto.NeedsAck,
		HasAck:      proto.HasAck,
		AckCounter:  proto.AckCounter,
		Payload:     body[pl:],
	}
	// A message asking for an acknowledgement on a session without MRP is
	// treated as not asking (MessageExchange.ts:449-453).
	if msg.RequiresAck && !s.UsesMRP() {
		msg.RequiresAck = false
	}
	m.route(s, msg, duplicate)
}

// openSession resolves the session of a received message and decrypts
// it. ok is false when the message is dropped; inbound is true when the
// session was created for this message.
func (m *Manager) openSession(hdr *message.Header, body *[]byte, src *net.UDPAddr) (s *Session, duplicate, inbound, ok bool) {
	if hdr.SessionID == UnsecuredSessionID {
		// A response names our ephemeral node id as destination; a
		// request names the peer's as source (ExchangeManager.ts:244-265).
		var initiator uint64
		hasDest := hdr.DestSize == message.DestNodeID
		switch {
		case hasDest:
			initiator = hdr.DestNodeID
		case hdr.HasSourceNodeID:
			initiator = hdr.SourceNodeID
		}
		m.mu.Lock()
		s = m.unsecured[initiator]
		m.mu.Unlock()
		if s == nil {
			if hasDest {
				// A late response to a session that no longer exists
				// (ExchangeManager.ts:250-259).
				m.log.Debug("matter.controller.drop", slog.String("reason", "unsecured response for unknown session"))
				return nil, false, false, false
			}
			if s = m.newInboundSession(hdr, src); s == nil {
				return nil, false, false, false
			}
			inbound = true
		}
		return s, !s.window.Accept(hdr.MessageCounter), inbound, true
	}
	if s = m.Session(hdr.SessionID); s == nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "unknown session"), slog.Int("session_id", int(hdr.SessionID)))
		return nil, false, false, false
	}
	plain, dup, err := s.secure.Decrypt(hdr, hdr.NonceSecurityFlags(), *body)
	if err != nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "decrypt"), slog.Any("err", err))
		return nil, false, false, false
	}
	*body = plain
	return s, dup, false, true
}

// newInboundSession creates the unsecured session of a peer's initiating
// message, keyed by the peer's source node id — matter.js
// ExchangeManager.ts:261-264 (createUnsecuredSession with the initiator's
// node id). A request without a source node id is dropped: the session
// could not be told apart from another peer's.
func (m *Manager) newInboundSession(hdr *message.Header, src *net.UDPAddr) *Session {
	if !hdr.HasSourceNodeID {
		m.log.Debug("matter.controller.drop", slog.String("reason", "unsecured request without source node id"))
		return nil
	}
	s := &Session{
		m:         m,
		addr:      src,
		nodeID:    hdr.SourceNodeID,
		inbound:   true,
		window:    mrp.NewWindow(),
		params:    PeerParameters(nil),
		exchanges: make(map[*Exchange]struct{}),
		closing:   make(chan struct{}),
	}
	s.timestamp = m.clock.Now()
	s.createdAt = s.timestamp
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if _, taken := m.unsecured[s.nodeID]; taken {
		return nil
	}
	m.unsecured[s.nodeID] = s
	return s
}

// releaseInbound closes an inbound session no exchange holds —
// ExchangeManager.ts:210-216.
func (m *Manager) releaseInbound(s *Session) {
	if !s.IsClosed() && len(s.liveExchanges()) == 0 {
		s.Close()
	}
}

// route hands msg to the exchange it belongs to — its index sets the
// exchanges we initiated apart (ExchangeManager.ts:331-343) — or treats it
// as unsolicited.
func (m *Manager) route(s *Session, msg *Message, duplicate bool) {
	idx := uint32(msg.ExchangeID)
	if !msg.Initiator {
		idx |= initiatedExchangeFlag
	}
	m.mu.Lock()
	x := m.exchanges[idx]
	m.mu.Unlock()
	if x != nil && x.session != s {
		x = nil
	}
	if x != nil {
		if x.considerClosed() && !msg.isStandaloneAck() {
			// ExchangeManager.ts:359-378: acknowledge, then close.
			x.sendStandaloneAckFor(msg)
			x.Close()
			return
		}
		x.onMessageReceived(msg, duplicate)
		return
	}
	// A new exchange on a closing session is declined
	// (ExchangeManager.ts:386-389).
	if s.IsClosed() || s.IsPeerLost() {
		m.log.Debug("matter.controller.drop", slog.String("reason", "session closing"), slog.Int("session_id", int(s.id)))
		return
	}
	// A Secure Channel message that opens an exchange on a secure session
	// goes to the Secure Channel handler (ExchangeManager.ts:407-440); an
	// unsolicited standalone ack opens nothing (:413-420).
	if s.secure != nil && msg.ProtocolID == mrp.SecureChannelProtocolID && msg.Initiator && !duplicate {
		if msg.isStandaloneAck() && !msg.RequiresAck {
			m.log.Debug("matter.controller.drop", slog.String("reason", "unsolicited standalone ack"), slog.Int("session_id", int(s.id)))
			return
		}
		x := m.accept(s, msg)
		x.onMessageReceived(msg, false)
		m.handleSecureChannel(x, msg)
		return
	}
	// A handler takes the first message of an exchange the peer opens
	// (ExchangeManager.ts:407-440): on an unsecured session a Sigma1 or a
	// PBKDFParamRequest, on a secure session any other protocol's message.
	if msg.Initiator && !duplicate {
		if h := m.handlerFor(s, msg); h != nil {
			m.dispatch(s, msg, h)
			return
		}
	}
	// Anything else is acknowledged when it asks for it
	// (ExchangeManager.ts:441-452) and dropped otherwise (:453-473).
	if msg.RequiresAck {
		m.sendAck(s, msg, 0)
	}
	m.log.Debug("matter.controller.unsolicited",
		slog.Int("session_id", int(s.id)), slog.Int("exchange_id", int(msg.ExchangeID)),
		slog.Int("protocol_id", int(msg.ProtocolID)), slog.Int("opcode", int(msg.Opcode)), slog.Bool("duplicate", duplicate))
}

// handlerFor is the handler of the exchange msg opens on s, nil for
// none: on an unsecured session the installed Secure Channel handler for
// a Sigma1 (SecureChannelProtocol.ts:146-148) and the built-in refusal for
// a PBKDFParamRequest (:129-137); on a secure session the installed
// handler of any protocol but the Secure Channel, whose secure-session
// handler is built in.
func (m *Manager) handlerFor(s *Session, msg *Message) ProtocolHandler {
	if msg.ProtocolID == mrp.SecureChannelProtocolID {
		if s.secure != nil {
			return nil
		}
		switch msg.Opcode {
		case mrp.SCOpcodeSigma1:
			return m.handler(mrp.SecureChannelProtocolID)
		case mrp.SCOpcodePBKDFParamRequest:
			return m.refusePASE
		}
		return nil
	}
	if s.secure == nil {
		return nil
	}
	return m.handler(msg.ProtocolID)
}

// dispatch opens the exchange msg starts, lets it take msg — owing the
// acknowledgement — and runs h on a goroutine of its own
// (ExchangeManager.ts:423-437: fromInitialMessage, #addExchange,
// onMessageReceived, then onNewExchange). Once h returns the exchange is
// closed, and an inbound unsecured session no exchange holds any longer is
// closed with it (:205-217). The handler runs on a goroutine of the
// manager, which [Manager.Shutdown] waits for; a manager closed meanwhile
// starts no handler and closes the exchange.
func (m *Manager) dispatch(s *Session, msg *Message, h ProtocolHandler) {
	x := m.accept(s, msg)
	x.onMessageReceived(msg, false)
	x.takeFirst(msg)
	if !m.goWork(func() {
		h(x, msg)
		x.Close()
		if s.inbound {
			<-x.Done()
			m.releaseInbound(s)
		}
	}) {
		x.Close()
	}
}

// refusePASE answers a PBKDFParamRequest with a StatusReport
// InvalidParam: the controller runs no PASE server. Mirrors matter.js
// SecureChannelProtocol.ts:129-137 (SecureChannelMessenger.sendError,
// :170-172, a reliable StatusReport with general code Failure), then the
// exchange closes.
func (m *Manager) refusePASE(x *Exchange, _ *Message) {
	report := StatusReport{GeneralCode: mrp.SCStatusGeneralFailure, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: mrp.SCStatusProtocolInvalidParameter}
	if err := x.Send(context.Background(), mrp.SCOpcodeStatusReport, report.Encode(), SendOptions{}); err != nil {
		m.log.Debug("matter.controller.pase_refusal_failed", slog.Int("exchange_id", int(x.id)), slog.Any("err", err))
	}
}

// sendAck sends a standalone ack for msg on s, outside any exchange.
// dest is the destination node id of an unsecured ack, 0 for none.
func (m *Manager) sendAck(s *Session, msg *Message, dest uint64) {
	proto := message.ProtocolHeader{
		Initiator:  !msg.Initiator,
		Opcode:     mrp.StandaloneAckOpcode,
		ExchangeID: msg.ExchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
		HasAck:     true,
		AckCounter: msg.Counter,
	}
	datagram, _, err := m.seal(s, proto, nil, dest)
	if err != nil {
		return
	}
	_ = m.conn.Send(s.Addr(), datagram)
}

// seal frames one message on s: on the unsecured session with the global
// counter and our ephemeral node id as source (or dest as destination),
// on a secure session encrypted under the peer's session id. It returns
// the datagram and the message counter.
func (m *Manager) seal(s *Session, proto message.ProtocolHeader, payload []byte, dest uint64) (datagram []byte, counter uint32, err error) {
	plain := append(proto.Marshal(), payload...)
	if s.secure == nil {
		hdr := message.Header{MessageCounter: m.counter.Next()}
		switch {
		case dest != 0:
			hdr.DestSize, hdr.DestNodeID = message.DestNodeID, dest
		case s.inbound:
			// Our messages on a session the peer opened name the peer's
			// ephemeral node id as destination (ExchangeManager.ts:244-247).
			hdr.DestSize, hdr.DestNodeID = message.DestNodeID, s.nodeID
		case s.nodeID != 0:
			hdr.HasSourceNodeID, hdr.SourceNodeID = true, s.nodeID
		}
		return append(hdr.Marshal(), plain...), hdr.MessageCounter, nil
	}
	hdr := message.Header{SessionID: s.peer}
	sealed, err := s.secure.Encrypt(&hdr, hdr.SecurityFlags(), plain)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrSessionClosed, err)
	}
	return append(hdr.Marshal(), sealed.Ciphertext...), sealed.Counter, nil
}

// reportPeerLost calls the PeerLost hook once for a secure session that
// became lost ([Session.HandlePeerLoss]).
func (m *Manager) reportPeerLost(s *Session, cause error) {
	if m.peerLost == nil || s.secure == nil {
		return
	}
	m.peerLost(s, cause)
}

// exchangePeerLost is the peer loss an exchange observed. Mirrors matter.js
// ExchangeManager.ts:556-579 (MessageExchangeContext.peerLost): ignored
// off a secure session; a session without a peer address (PASE, or no
// node id) alone is lost, sparing the exchange; otherwise every session of
// the peer created before the exchange is (SessionManager.ts:636-638,
// asOf the exchange's context creation, :549).
func (m *Manager) exchangePeerLost(x *Exchange, cause error) {
	s := x.session
	if s.secure == nil {
		return
	}
	if s.fabricIndex == 0 || s.PeerNodeID() == 0 {
		s.handlePeerLoss(cause, x)
		return
	}
	m.handlePeerLoss(PeerAddress{FabricIndex: s.fabricIndex, NodeID: s.PeerNodeID()}, cause, x.createdAt, x)
}

// PeerAddress names a peer on one of our fabrics: our fabric index and
// the peer's operational node id — matter.js PeerAddress
// (packages/protocol/src/peer/PeerAddress.ts); handshake.PeerAddress and
// the peer package use this type.
type PeerAddress struct {
	FabricIndex uint8
	NodeID      uint64
}

// SessionsFor returns the open secure sessions with peer — matter.js
// SessionManager.sessionsFor.
func (m *Manager) SessionsFor(peer PeerAddress) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Session
	for _, s := range m.sessions {
		if s.fabricIndex == peer.FabricIndex && s.secure.PeerNodeID() == peer.NodeID && peer.FabricIndex != 0 {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *Session) int { return a.createdAt.Compare(b.createdAt) })
	return out
}

// HandlePeerLoss gives up every secure session with peer created before
// asOf (a zero asOf: all of them) — each as [Session.HandlePeerLoss] does.
// Mirrors matter.js SessionManager.ts:636-638 (handlePeerLoss, asOf
// defaulting to now) and :677-696 (#handlePeerLoss: a session created at
// or after asOf is kept).
func (m *Manager) HandlePeerLoss(peer PeerAddress, cause error, asOf time.Time) {
	m.handlePeerLoss(peer, cause, asOf, nil)
}

func (m *Manager) handlePeerLoss(peer PeerAddress, cause error, asOf time.Time, current *Exchange) {
	for _, s := range m.SessionsFor(peer) {
		if !asOf.IsZero() && !s.createdAt.Before(asOf) {
			continue
		}
		s.handlePeerLoss(cause, current)
	}
}

// accept registers the exchange the peer opened with msg — matter.js
// MessageExchange.fromInitialMessage and #addExchange
// (ExchangeManager.ts:423-434, :494-508).
func (m *Manager) accept(s *Session, msg *Message) *Exchange {
	x := newExchange(m, s, msg.ExchangeID, msg.ProtocolID, false, Options{})
	m.mu.Lock()
	m.exchanges[x.index] = x
	m.mu.Unlock()
	s.addExchange(x)
	m.cleanupSessionExchanges(s)
	return x
}

// handleSecureChannel is the Secure Channel handler of a controller for an
// exchange the peer opened on a secure session. Mirrors matter.js
// packages/protocol/src/securechannel/SecureChannelProtocol.ts:30-88
// (StatusReportOnlySecureChannelProtocol) at 0ceb202d: an initial
// StatusReport Success / CloseSession means the peer closed the session —
// NodeSession.handlePeerClose (NodeSession.ts:305-309), the session lost
// with a [PeerInitiatedCloseError] — and the exchange that carried it
// closes, acknowledging it; any other initial StatusReport is ignored
// (:69-77); a standalone ack ends there, unacknowledged as every
// standalone ack (:43-45, MessageExchange.ts:519-522);
// any other message is refused as unexpected (:46-49). Every branch closes
// the exchange, so an ack the message asked for goes out.
func (m *Manager) handleSecureChannel(x *Exchange, msg *Message) {
	switch {
	case msg.Opcode == mrp.SCOpcodeStatusReport:
		r, ok := DecodeStatusReport(msg.Payload)
		if !ok || r.GeneralCode != mrp.SCStatusGeneralSuccess || r.ProtocolCode != statusCloseSession {
			m.log.Debug("matter.controller.secure_channel.ignored_status",
				slog.Int("session_id", int(x.session.id)), slog.Int("exchange_id", int(x.id)),
				slog.Int("general_code", int(r.GeneralCode)), slog.Int("protocol_code", int(r.ProtocolCode)))
			x.Close()
			return
		}
		m.log.Debug("matter.controller.secure_channel.closed_by_peer", slog.Int("session_id", int(x.session.id)))
		x.session.handlePeerLoss(&PeerInitiatedCloseError{}, x)
		x.Close()
	case msg.isStandaloneAck():
		x.Close()
	default:
		m.log.Warn("matter.controller.secure_channel.unexpected",
			slog.Int("session_id", int(x.session.id)), slog.Int("exchange_id", int(x.id)), slog.Int("opcode", int(msg.Opcode)),
			slog.Any("err", ErrFlow))
		x.Close()
	}
}
