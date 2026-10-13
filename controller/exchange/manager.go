// SPDX-License-Identifier: MIT
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
	"sync"

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
	// Logger receives debug diagnostics; nil discards them.
	Logger *slog.Logger
	// PeerLost is called when a send or a wait on an exchange of a secure
	// session fails in a way that shows the peer is gone — matter.js
	// MessageExchangeContext.peerLost (ExchangeManager.ts:556-579), fired
	// from MessageExchange.ts:613-621 and :822-832. Nil ignores it.
	PeerLost func(s *Session, cause error)
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

	// counter is the global unencrypted message counter every unsecured
	// session sends with (SessionManager.ts:265, :439).
	counter *mrp.Counter

	mu             sync.Mutex
	closed         bool
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
		conn:     conn,
		clock:    cfg.Clock,
		rand:     cfg.Rand,
		log:      cfg.Logger,
		local:    DefaultLocalParameters(),
		peerLost: cfg.PeerLost,
		counter:  counter,
		sessions: make(map[uint16]*Session),
		// unsecured is keyed by our ephemeral initiator node id
		// (SessionManager.ts:446-454).
		unsecured: make(map[uint64]*Session),
		exchanges: make(map[uint32]*Exchange),
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
	if m.rand == nil {
		m.rand = mrand.Float64
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	return m, nil
}

// LocalParameters is our session parameters.
func (m *Manager) LocalParameters() sessionparams.Parameters { return m.local }

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
	}
	s.timestamp = m.clock.Now()
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

// CloseSession ends a session: a secure session first sends CloseSession
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
	if s.secure != nil {
		if x, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{}); err == nil {
			report := StatusReport{
				GeneralCode:  mrp.SCStatusGeneralSuccess,
				ProtocolID:   uint32(mrp.SecureChannelProtocolID),
				ProtocolCode: statusCloseSession,
			}
			if err := x.Send(ctx, mrp.SCOpcodeStatusReport, report.Encode(), SendOptions{SuppressAck: true}); err != nil {
				m.log.Debug("matter.controller.close_session_failed", slog.Int("session", int(s.id)), slog.String("err", err.Error()))
			}
			x.Close()
		}
	}
	s.Close()
}

// statusCloseSession is the Secure Channel protocol code CloseSession
// (Matter §4.10.1.3; bridge/bridgetest/transport.go:211).
const statusCloseSession uint16 = 0x0003

// Close closes every exchange and session without telling the peers. It
// is idempotent; the manager accepts no new session afterwards.
func (m *Manager) Close() {
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
	x := newExchange(m, s, id, protocol, opts)
	m.exchanges[uint32(id)|initiatedExchangeFlag] = x
	m.mu.Unlock()
	s.addExchange(x)
	m.cleanupSessionExchanges(s)
	return x, nil
}

// removeExchange drops x from the table if the index still maps to it.
func (m *Manager) removeExchange(x *Exchange) {
	idx := uint32(x.id) | initiatedExchangeFlag
	m.mu.Lock()
	if m.exchanges[idx] == x {
		delete(m.exchanges, idx)
	}
	m.mu.Unlock()
	x.session.deleteExchange(x)
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
	m.log.Debug("matter.controller.exchange_evicted", slog.Int("session", int(s.id)), slog.Int("exchange", int(lru.id)))
	go lru.Close()
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
		m.log.Debug("matter.controller.drop", slog.String("reason", "header"), slog.String("err", err.Error()))
		return
	}
	if hdr.SessionType != message.SessionUnsecured || hdr.Privacy {
		// No group sessions; a unicast message with the privacy flag is
		// invalid (ExchangeManager.ts:230-235).
		m.log.Debug("matter.controller.drop", slog.String("reason", "session type"))
		return
	}
	body := append([]byte(nil), buf[hl:]...)
	s, duplicate, ok := m.openSession(&hdr, &body, src)
	if !ok {
		return
	}
	proto, pl, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "protocol header"), slog.String("err", err.Error()))
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
	m.route(s, msg, duplicate)
}

// openSession resolves the session of a received message and decrypts
// it. ok is false when the message is dropped.
func (m *Manager) openSession(hdr *message.Header, body *[]byte, src *net.UDPAddr) (s *Session, duplicate, ok bool) {
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
			if !hasDest {
				m.unsolicitedUnsecured(hdr, *body, src)
			} else {
				m.log.Debug("matter.controller.drop", slog.String("reason", "unsecured response for unknown session"))
			}
			return nil, false, false
		}
		return s, !s.window.Accept(hdr.MessageCounter), true
	}
	if s = m.Session(hdr.SessionID); s == nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "unknown session"), slog.Int("session", int(hdr.SessionID)))
		return nil, false, false
	}
	plain, dup, err := s.secure.Decrypt(hdr, hdr.NonceSecurityFlags(), *body)
	if err != nil {
		m.log.Debug("matter.controller.drop", slog.String("reason", "decrypt"), slog.String("err", err.Error()))
		return nil, false, false
	}
	*body = plain
	return s, dup, true
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
	// No exchange of ours: the controller of stage 1 serves no protocol,
	// so the message is acknowledged when it asks for it
	// (ExchangeManager.ts:441-452) and dropped otherwise (:453-473).
	if msg.RequiresAck && !s.IsClosed() {
		m.sendAck(s, msg, 0)
	}
	m.log.Debug("matter.controller.unsolicited",
		slog.Int("session", int(s.id)), slog.Int("exchange", int(msg.ExchangeID)),
		slog.Int("protocol", int(msg.ProtocolID)), slog.Int("opcode", int(msg.Opcode)), slog.Bool("duplicate", duplicate))
}

// unsolicitedUnsecured handles an unsecured message a peer initiates
// towards us: it is acknowledged when it asks for it, nothing else
// (ExchangeManager.ts:261-264 creates a session for it that the handler
// of :441-452 then only acknowledges on).
func (m *Manager) unsolicitedUnsecured(hdr *message.Header, body []byte, src *net.UDPAddr) {
	proto, _, err := message.UnmarshalProtocolHeader(body)
	if err != nil || !proto.NeedsAck || !hdr.HasSourceNodeID {
		return
	}
	s := &Session{m: m, addr: src, exchanges: make(map[*Exchange]struct{})}
	m.sendAck(s, &Message{Counter: hdr.MessageCounter, ExchangeID: proto.ExchangeID, Initiator: proto.Initiator, RequiresAck: true}, hdr.SourceNodeID)
	m.log.Debug("matter.controller.unsolicited", slog.Int("session", 0), slog.Int("exchange", int(proto.ExchangeID)))
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

// reportPeerLost calls the PeerLost hook for a secure session; matter.js
// ExchangeManager.ts:556-560 ignores a peer loss on any other session.
func (m *Manager) reportPeerLost(s *Session, cause error) {
	if m.peerLost == nil || s.secure == nil {
		return
	}
	m.peerLost(s, cause)
}
