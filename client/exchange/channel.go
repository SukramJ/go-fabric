// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"errors"
	"net"
)

// Channel is a connected carrier a session sends on in place of the
// manager's socket: a TCP connection (a *transport/tcp.Conn satisfies it).
// matter.js binds every session to a channel — the UDP socket's address or
// a TcpChannel (packages/protocol/src/transport/tcp/TcpChannel.ts:43 at
// 862ad75f); here a session without one sends on the manager's [Conn].
type Channel interface {
	// Send sends one framed message.
	Send(msg []byte) error
	// Reliable reports a carrier the layers above run no MRP over
	// (Session.ts:355-356) — true for TCP.
	Reliable() bool
	// MaxMessageSize is the largest message Send accepts.
	MaxMessageSize() int
	// Remote is the peer's address.
	Remote() net.Addr
	// Close closes the connection.
	Close() error
	// Closed is closed once the connection is.
	Closed() <-chan struct{}
}

// Transport is the carrier of a session — matter.js ChannelType
// (packages/general/src/net/Channel.ts).
type Transport uint8

// The transports.
const (
	TransportUDP Transport = iota
	TransportTCP
	TransportBLE
)

func (t Transport) String() string {
	switch t {
	case TransportTCP:
		return "tcp"
	case TransportBLE:
		return "ble"
	case TransportUDP:
	}
	return "udp"
}

// ErrTransportClosed is the cause an exchange or session of a TCP
// connection is closed with when the connection drops — matter.js
// TransportClosedError("TCP connection dropped")
// (packages/protocol/src/protocol/ExchangeManager.ts:651-665).
var ErrTransportClosed = errors.New("exchange: TCP connection dropped")

// AttachChannel registers ch with the manager and returns the address that
// names it: a session the manager opens on this address —
// [Manager.NewUnsecuredSession], [Manager.NewSecureSession], one a
// message received on ch creates — is bound to ch, sends on it, runs no
// MRP when it is reliable and never moves to another address. The address
// is a fresh value equal to ch's remote address; its identity, not its
// value, binds. When ch closes, every session bound to it is closed with
// [ErrTransportClosed] (ExchangeManager.ts:628-666); when the last of
// them closes, so does ch (:668-710). It is matter.js's ServerAddress
// with type "tcp" a session's channel is opened for
// (packages/protocol/src/peer/PeerSet.ts:262-300). Attaching ch again
// returns its address.
func (m *Manager) AttachChannel(ch Channel) *net.UDPAddr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.channelAddrs[ch]; ok {
		return a
	}
	a := &net.UDPAddr{}
	switch r := ch.Remote().(type) {
	case *net.TCPAddr:
		a = &net.UDPAddr{IP: r.IP, Port: r.Port, Zone: r.Zone}
	case *net.UDPAddr:
		*a = *r
	}
	m.channels[a] = ch
	m.channelAddrs[ch] = a
	if !m.closed {
		m.work.Go(func() { m.watchChannel(ch) })
	}
	return a
}

// channelOf is the channel addr names, nil for an address of the socket.
func (m *Manager) channelOf(addr *net.UDPAddr) Channel {
	if addr == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.channels[addr]
}

// watchChannel evicts ch's sessions once it closes (ExchangeManager.ts:628-666).
func (m *Manager) watchChannel(ch Channel) {
	<-ch.Closed()
	m.mu.Lock()
	a := m.channelAddrs[ch]
	delete(m.channelAddrs, ch)
	delete(m.channels, a)
	var bound []*Session
	for _, s := range m.sessions {
		if s.channel == ch {
			bound = append(bound, s)
		}
	}
	for _, s := range m.unsecured {
		if s.channel == ch {
			bound = append(bound, s)
		}
	}
	m.mu.Unlock()
	for _, s := range bound {
		m.log.Debug("matter.controller.tcp_session_evicted", "session_id", int(s.id))
		s.closeWith(ErrTransportClosed)
	}
}

// closeChannelIfLast closes ch when no session is bound to it any more —
// ExchangeManager.ts:668-710 (#closeTcpChannelIfLastSession), CHIP's
// behaviour.
func (m *Manager) closeChannelIfLast(ch Channel) {
	m.mu.Lock()
	for _, s := range m.sessions {
		if s.channel == ch {
			m.mu.Unlock()
			return
		}
	}
	for _, s := range m.unsecured {
		if s.channel == ch {
			m.mu.Unlock()
			return
		}
	}
	m.mu.Unlock()
	m.log.Debug("matter.controller.tcp_last_session_closed")
	_ = ch.Close()
}

// HandleChannelMessage processes one message received on ch: the
// transport/tcp Handler of a connection the manager's sessions use. A
// channel not yet attached — an inbound connection — is attached first, so
// the session the message opens is bound to it.
func (m *Manager) HandleChannelMessage(ch Channel, msg []byte) {
	m.HandleDatagram(msg, m.AttachChannel(ch))
}

// send transmits datagram on s: on its channel, else on the socket to dest
// (an address override or the session's address).
func (m *Manager) send(s *Session, dest *net.UDPAddr, datagram []byte) error {
	if s.channel != nil {
		return s.channel.Send(datagram)
	}
	return m.conn.Send(dest, datagram)
}

// Transport is the carrier of the session: TCP for a session bound to a
// channel, BLE on a reliable manager (a BTP channel), UDP otherwise.
func (s *Session) Transport() Transport {
	switch {
	case s.channel != nil:
		return TransportTCP
	case s.m.reliable:
		return TransportBLE
	}
	return TransportUDP
}

// MaxPayloadSize is the largest message payload an exchange of the
// session sends: [MaxPayloadSize] over UDP, the channel's limit less the
// Matter message overhead over TCP (MessageExchange.ts:435-439, :156).
func (s *Session) MaxPayloadSize() int {
	if s.channel != nil {
		return s.channel.MaxMessageSize() - (26 + 12 + 16)
	}
	return MaxPayloadSize
}
