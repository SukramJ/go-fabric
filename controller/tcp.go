// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"net"
	"strconv"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/tcp"
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
}

// localParameters are our session parameters with the TCP roles: TCP is
// reported in our session parameters, not only over DNS-SD, so a peer
// learns it during PASE and CASE (ServerNetworkRuntime.ts:355-364); the
// MAX_TCP_MESSAGE_SIZE field only when a TCP role is (SessionManager.ts:346-359).
func localParameters(t TCPConfig) *sessionparams.Parameters {
	if !t.Incoming && !t.Outgoing {
		return nil
	}
	p := exchange.DefaultLocalParameters()
	if t.Outgoing {
		p.SupportedTransports |= sessionparams.TransportTCPClient
	}
	if t.Incoming {
		p.SupportedTransports |= sessionparams.TransportTCPServer
	}
	p.MaxTCPMessageSize = sessionparams.DefaultMaxTCPMessageSize
	return &p
}

// dialTCP is the peers' TCP dialer: each connection hands its messages to
// the exchange manager — matter.js TcpTransport.openChannel
// (packages/protocol/src/transport/tcp/TcpTransport.ts:149-182).
func (c *Controller) dialTCP() func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
	d := &tcp.Dialer{Logger: c.log}
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

// listenTCP opens the TCP listener on the UDP socket's port and serves it
// into the exchange manager (ServerNetworkRuntime.ts:203-226).
func (c *Controller) listenTCP(ctx context.Context) error {
	ua := c.udp.LocalAddr()
	l, err := tcp.Listen(tcp.Config{
		LocalAddr: net.JoinHostPort(ua.IP.String(), strconv.Itoa(ua.Port)),
		Logger:    c.log, Clock: c.clock,
	})
	if err != nil {
		return err
	}
	mgr := c.mgr
	serveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, func(conn *tcp.Conn, msg []byte, _ func()) { mgr.HandleChannelMessage(conn, msg) }, nil)
	}()
	c.tcpAddr = l.Addr()
	c.stopTCP = func() { cancel(); _ = l.Close(); <-served }
	return nil
}

// TCPAddr is the controller's TCP listener address, nil without
// TCPConfig.Incoming.
func (c *Controller) TCPAddr() *net.TCPAddr { return c.tcpAddr }
