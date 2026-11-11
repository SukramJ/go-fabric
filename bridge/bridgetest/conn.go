// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

import (
	"errors"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/internal/bridgeseam"
	"github.com/SukramJ/go-fabric/transport"
)

// ErrNoConn is returned by [ServeConn] for a nil connection.
var ErrNoConn = errors.New("bridgetest: nil connection")

// ConnServer serves the Matter messages of one connection of a transport
// the bridge does not listen on itself through the bridge's
// transport-neutral receive path: replies go back on the connection,
// without MRP when it is reliable. It is the receiver the bridge's own BLE
// transport serves each central's BTP session with (bridge.Config.BLE),
// handed to a test that holds a connection of its own — a
// controller/ble/bletest peripheral's session, a fault model's.
type ConnServer struct {
	fns *connFns
}

// connFns are the bridge's receiver of the connection.
type connFns struct {
	deliver     func([]byte)
	close, wait func()
}

// ServeConn returns the server of conn's messages on b.
func ServeConn(b *bridge.Bridge, conn transport.Conn) (*ConnServer, error) {
	if b == nil {
		return nil, ErrNoBridge
	}
	if conn == nil {
		return nil, ErrNoConn
	}
	deliver, closeConn, wait, ok := bridgeseam.ServeConn(b, conn)
	if !ok {
		return nil, ErrNoBridge
	}
	return &ConnServer{fns: &connFns{deliver: deliver, close: closeConn, wait: wait}}, nil
}

// Deliver hands one complete Matter message to the bridge. Messages are
// handled in delivery order, each once its predecessor returned or
// released its turn — the contract of transport/tcp's ordered handler, so
// a handler that waits for the peer's answer on the same connection lets
// that answer through. Deliver does not wait for the handler.
func (s *ConnServer) Deliver(msg []byte) { s.fns.deliver(msg) }

// Close stops delivering and evicts every session bound to the
// connection, as a dropped TCP connection does.
func (s *ConnServer) Close() { s.fns.close() }

// Wait waits for every delivered message's handler to return.
func (s *ConnServer) Wait() { s.fns.wait() }
