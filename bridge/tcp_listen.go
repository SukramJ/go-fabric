// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"syscall"

	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TCPConfig selects the Matter TCP transport (ADR 0019). Incoming opens
// a TCP listener on the UDP socket's port; Outgoing declares that this
// node may open TCP connections itself. Both are advertised in the
// operational record's T key (bit 1 client, bit 2 server), which is
// absent when neither is set. The zero value is TCP off — matter.js NetworkServer.resolveTcpConfig with
// nothing configured (NetworkServer.ts:261-272) — and leaves the bridge
// exactly as it was before TCP existed.
type TCPConfig struct {
	// Incoming accepts TCP connections (the "tcpServer" role).
	Incoming bool
	// Outgoing may open TCP connections (the "tcpClient" role).
	Outgoing bool
	// MaxInboundConnections is the most inbound connections the TCP
	// listener holds in all, whatever their source addresses; at the
	// bound a new connection takes the place of the held one idle longest
	// (no message received or sent for the longest time) among those that
	// carried no traffic within the secure session's idle timeout (5
	// minutes), or is closed when every held one did. Zero is the
	// device's default, tcp.MaxInboundConnections (4, connectedhomeip's
	// CHIP_CONFIG_MAX_ACTIVE_TCP_CONNECTIONS); a negative value is refused
	// by [New]. A bridge that several controllers reach over TCP may raise
	// it.
	MaxInboundConnections int
}

// maxInboundConnections is the listener's total bound t selects: the
// configured one, tcp.MaxInboundConnections for zero.
func (t TCPConfig) maxInboundConnections() int {
	if t.MaxInboundConnections == 0 {
		return tcp.MaxInboundConnections
	}
	return t.MaxInboundConnections
}

// maxPortAssignmentRetries bounds the attempts to find a port free for
// both UDP and TCP when the port is auto-assigned. Mirrors matter.js
// packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:54
// (MAX_PORT_ASSIGNMENT_RETRIES = 10), applied only to an auto-assigned
// port (:164).
const maxPortAssignmentRetries = 10

// openTransports binds the UDP socket and, with TCP incoming, the TCP
// listener on the same port. Mirrors matter.js
// ServerNetworkRuntime.ts:203-226: the TCP transport listens on the UDP
// interface's port; when that port is taken for TCP and the port was
// auto-assigned, both sockets are closed and the pair is tried again.
func (b *Bridge) openTransports() (udpListener *udp.Listener, tcpListener *tcp.Listener, err error) {
	if open := b.openUDP.Load(); open != nil && !b.cfg.TCP.Incoming {
		udpListener, err = (*open)()
		return udpListener, nil, err
	}
	udpCfg := udp.Config{LocalAddr: b.cfg.Listen, PreferIPv4: b.cfg.PreferIPv4, Logger: b.logger}
	if !b.cfg.TCP.Incoming {
		udpListener, err = udp.New(udpCfg)
		return udpListener, nil, err
	}
	attempts := 1
	if autoAssignedPort(b.cfg.Listen) {
		attempts = maxPortAssignmentRetries
	}
	for attempt := 1; ; attempt++ {
		udpListener, err = udp.New(udpCfg)
		if err != nil {
			return nil, nil, err
		}
		port := udpListener.LocalAddr().Port
		tcpListener, err = tcp.Listen(tcp.Config{
			LocalAddr:  tcpBindAddr(b.cfg.Listen, b.cfg.PreferIPv4, port),
			PreferIPv4: b.cfg.PreferIPv4,
			Logger:     b.logger,
			Clock:      b.clk(),

			MaxInboundConnections: b.cfg.TCP.maxInboundConnections(),
		})
		if err == nil {
			return udpListener, tcpListener, nil
		}
		_ = udpListener.Close()
		if !errors.Is(err, syscall.EADDRINUSE) || attempt >= attempts {
			return nil, nil, fmt.Errorf("tcp: %w", err)
		}
		b.logger.Info("matter.tcp.port_in_use",
			slog.Int("port", port),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", attempts))
	}
}

// autoAssignedPort reports whether listen leaves the port to the OS.
func autoAssignedPort(listen string) bool {
	if listen == "" {
		return false
	}
	_, port, err := net.SplitHostPort(listen)
	return err == nil && port == "0"
}

// tcpBindAddr is the TCP bind address beside a UDP socket bound per
// listen: the same host, the UDP socket's port.
func tcpBindAddr(listen string, preferIPv4 bool, port int) string {
	host := ""
	if listen != "" {
		if h, _, err := net.SplitHostPort(listen); err == nil {
			host = h
		}
	}
	if host == "" {
		host = "::"
		if preferIPv4 {
			host = "0.0.0.0"
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
