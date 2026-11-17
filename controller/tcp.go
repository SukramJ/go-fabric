// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"strconv"
	"syscall"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TCPConfig selects the controller's Matter TCP roles — matter.js
// NetworkServer.State.tcp resolved by resolveTcpConfig
// (packages/node/src/behavior/system/network/NetworkServer.ts:261-272 at
// 862ad75f; the state :211-217). The zero value is TCP off.
type TCPConfig struct {
	// Incoming accepts TCP connections on the controller's UDP port (the
	// tcpServer role, ServerNetworkRuntime.ts:334-338; the listener :203-226).
	Incoming bool
	// Outgoing opens TCP connections to peers that serve TCP when an
	// interaction requires or prefers it (the tcpClient role).
	Outgoing bool
	// MaxInboundConnections is the most inbound connections the TCP
	// listener holds in all, whatever their source addresses; at the
	// bound a new connection takes the place of the held one idle longest
	// (no message received or sent for the longest time) among those that
	// carried no traffic within the secure session's idle timeout (5
	// minutes), or is closed when every held one did. Zero is the
	// controller's default, 256; a negative value is [ErrConfiguration].
	MaxInboundConnections int
	// MaxMessageSize is the largest TCP message, its 4-byte length prefix
	// not counted, the controller accepts and sends: its session
	// parameters state it as MAX_TCP_MESSAGE_SIZE, and every TCP
	// connection, inbound and outbound, holds to it. Zero is 64 000
	// (sessionparams.DefaultMaxTCPMessageSize). With a TCP role, a value
	// below exchange.MaxUDPMessageSize (1232) or above 2^32−1 is
	// [ErrConfiguration]; without one it is not used. Mirrors matter.js
	// NetworkServer.resolveTcpConfig and
	// SessionParameters.assertMaxTcpMessageSize
	// (packages/node/src/behavior/system/network/NetworkServer.ts:276-291,
	// packages/protocol/src/session/SessionParameters.ts:128-137 at
	// db021da0; review II, R2-P6-par-3209).
	MaxMessageSize int
}

// maxMessageSize is the TCP message bound t selects: the configured one,
// sessionparams.DefaultMaxTCPMessageSize for zero or without a TCP role
// (resolveTcpConfig, NetworkServer.ts:280-286 at db021da0).
func (t TCPConfig) maxMessageSize() int {
	if t.MaxMessageSize == 0 || (!t.Incoming && !t.Outgoing) {
		return int(sessionparams.DefaultMaxTCPMessageSize)
	}
	return t.MaxMessageSize
}

// validMessageSize reports whether t's MaxMessageSize is one its roles
// may use: anything without a TCP role, else zero or a whole number from
// exchange.MaxUDPMessageSize to the uint32 maximum
// (SessionParameters.ts:128-137 at db021da0).
func (t TCPConfig) validMessageSize() bool {
	if (!t.Incoming && !t.Outgoing) || t.MaxMessageSize == 0 {
		return true
	}
	return t.MaxMessageSize >= exchange.MaxUDPMessageSize && int64(t.MaxMessageSize) <= math.MaxUint32
}

// defaultMaxInboundTCPConnections is the controller's default total bound
// on inbound TCP connections: the bound on its inbound unsecured sessions
// (client/exchange/manager.go:949, maxInboundUnsecuredSessions = 256),
// owner decision 56 (review II). The device's default is the smaller
// tcp.MaxInboundConnections.
const defaultMaxInboundTCPConnections = 256

// maxInboundConnections is the listener's total bound t selects: the
// configured one, defaultMaxInboundTCPConnections for zero.
func (t TCPConfig) maxInboundConnections() int {
	if t.MaxInboundConnections == 0 {
		return defaultMaxInboundTCPConnections
	}
	return t.MaxInboundConnections
}

// localParameters are our session parameters with the TCP roles: TCP is
// reported in our session parameters, not only over DNS-SD, so a peer
// learns it during PASE and CASE (ServerNetworkRuntime.ts:355-364); the
// MAX_TCP_MESSAGE_SIZE field only when a TCP role is (SessionManager.ts:346-359).
func localParameters(t TCPConfig, s *SessionIntervals) *sessionparams.Parameters {
	if !t.Incoming && !t.Outgoing && s == nil {
		return nil
	}
	p := exchange.DefaultLocalParameters()
	s.apply(&p)
	if !t.Incoming && !t.Outgoing {
		return &p
	}
	if t.Outgoing {
		p.SupportedTransports |= sessionparams.TransportTCPClient
	}
	if t.Incoming {
		p.SupportedTransports |= sessionparams.TransportTCPServer
	}
	p.MaxTCPMessageSize = uint32(t.maxMessageSize()) //nolint:gosec // validMessageSize bounds it to uint32
	return &p
}

// dialTCP is the peers' TCP dialer: each connection hands its messages to
// the exchange manager — matter.js TcpTransport.openChannel
// (packages/protocol/src/transport/tcp/TcpTransport.ts:149-182).
func (c *Controller) dialTCP() func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
	d := &tcp.Dialer{Logger: c.log, MaxPayloadSize: c.tcp.maxMessageSize()}
	mgr := c.mgr
	return func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
		conn, err := d.Dial(ctx, addr.String(), func(conn *tcp.Conn, msg []byte, _ func()) {
			mgr.HandleChannelMessage(conn, msg)
		})
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

// maxPortAssignmentRetries bounds the attempts to find a port free for
// both UDP and TCP when the port is auto-assigned. Mirrors matter.js
// packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:54
// (MAX_PORT_ASSIGNMENT_RETRIES = 10) at db021da0, applied only to an
// auto-assigned port (:165); the bridge's openTransports
// (bridge/tcp_listen.go) applies the same bound.
const maxPortAssignmentRetries = 10

// tcpListen is the TCP listener's constructor; a test replaces it to make
// a bind fail (export_test.go).
var tcpListen = tcp.Listen

// bindTransports binds the UDP socket — the one given, or one per listen —
// and the TCP listener on its port. Mirrors matter.js
// ServerNetworkRuntime.ts:212-231 at db021da0 (MatterController.ts:349
// runs the controller as a ServerNode, whose runtime it is,
// ServerNode.ts:130-132): the TCP transport listens on the UDP
// interface's port, and when that port is taken for TCP and was
// auto-assigned the UDP socket is closed and the pair is bound again
// (matter.js closes its UDP interfaces, :224-227; the TCP listener that
// failed was never open), up to maxPortAssignmentRetries times. A socket the host gave is not
// replaced: its port is the host's choice, as a configured one is.
func (c *Controller) bindTransports(listen string, given *udp.Listener) (udpListener *udp.Listener, tcpListener *tcp.Listener, err error) {
	attempts := 1
	if given == nil && autoAssignedPort(listen) {
		attempts = maxPortAssignmentRetries
	}
	for attempt := 1; ; attempt++ {
		udpListener = given
		if udpListener == nil {
			if udpListener, err = udp.New(udp.Config{LocalAddr: listen}); err != nil {
				return nil, nil, err
			}
		}
		ua := udpListener.LocalAddr()
		tcpListener, err = tcpListen(tcp.Config{
			LocalAddr: net.JoinHostPort(ua.IP.String(), strconv.Itoa(ua.Port)),
			Logger:    c.log, Clock: c.clock,
			MaxInboundConnections: c.tcp.maxInboundConnections(),
			MaxPayloadSize:        c.tcp.maxMessageSize(),
		})
		if err == nil {
			return udpListener, tcpListener, nil
		}
		if given == nil {
			_ = udpListener.Close()
		}
		if !errors.Is(err, syscall.EADDRINUSE) || attempt >= attempts {
			return nil, nil, err
		}
		c.log.Info("matter.tcp.port_in_use",
			slog.Int("port", ua.Port),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", attempts))
	}
}

// autoAssignedPort reports whether listen leaves the port to the OS
// (ServerNetworkRuntime.ts:165, a port of 0). A listen without a port is
// not auto-assigned here: it fails SplitHostPort.
func autoAssignedPort(listen string) bool {
	_, port, err := net.SplitHostPort(listen)
	return err == nil && port == "0"
}

// serveTCP serves the bound TCP listener into the exchange manager
// (ServerNetworkRuntime.ts:212-231).
func (c *Controller) serveTCP(ctx context.Context, l *tcp.Listener) {
	mgr := c.mgr
	serveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	served := make(chan struct{})
	go func() {
		defer close(served)
		// Serve retries a failed accept and returns nil on its close, so
		// an error means TCP is not served: the host must see it (review
		// II, R2-P3-ctl-lan-04).
		if err := l.Serve(serveCtx, inboundTCPHandler(mgr), nil); err != nil {
			c.log.Warn("matter.tcp.serve.exit", slog.Any("err", err))
		}
	}()
	c.tcpAddr = l.Addr()
	c.stopTCP = func() { cancel(); _ = l.Close(); <-served }
}

// inboundTCPHandler is the TCP listener's handler: the message goes to the
// exchange manager, and a message that names a secure session bound to the
// connection it arrived on is noted as session traffic
// ([tcp.Conn.NoteSessionTraffic]), as the bridge notes its sessions'
// messages, so the listener keeps the place of a connection by its
// sessions' messages and not by any frame (review II, N3-finnet-4). A
// session is bound to a connection once it was established over it; a
// frame on that connection comes from the peer the session authenticated,
// whose connection it is.
func inboundTCPHandler(mgr *exchange.Manager) tcp.Handler {
	return func(conn *tcp.Conn, msg []byte, _ func()) {
		mgr.HandleChannelMessage(conn, msg)
		hdr, _, err := message.UnmarshalHeader(msg)
		if err != nil || hdr.SessionID == 0 {
			return
		}
		if s := mgr.Session(hdr.SessionID); s != nil && s.IsSecure() && s.Channel() == exchange.Channel(conn) {
			conn.NoteSessionTraffic()
		}
	}
}

// TCPAddr is the controller's TCP listener address, nil without
// TCPConfig.Incoming.
func (c *Controller) TCPAddr() *net.TCPAddr { return c.tcpAddr }
