// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/internal/udpseam"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// A bridge on Listen ":0" binds the dual-stack wildcard, and the port the
// OS assigns it may be one another socket holds on 127.0.0.1: measured on
// macOS, 562 of 3000 wildcard binds got a port among 3000 held on
// 127.0.0.1, and a datagram to 127.0.0.1:<port> then reaches the
// 127.0.0.1 socket, not the bridge (review II, F-db021da0-37). The
// bridges of newStartedBridge and newStartedBridgeWithSnapshotter, which
// tests reach over 127.0.0.1, are bound there; a test that counts what the
// bridge sends counts at the bridge's own socket.

// ownedSocket is a kernel UDP socket on 127.0.0.1 that a test hands the
// bridge through its listener seam (openUDP): it counts the datagrams the
// bridge writes and records the close.
type ownedSocket struct {
	*net.UDPConn
	writes atomic.Int32
	closed atomic.Bool
}

func (s *ownedSocket) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.writes.Add(1)
	return s.UDPConn.WriteToUDP(b, addr)
}

// Close closes the socket; closed is set once the address is free.
func (s *ownedSocket) Close() error {
	err := s.UDPConn.Close()
	s.closed.Store(true)
	return err
}

// useOwnedSocket makes b's Start serve a new socket on 127.0.0.1:0 and
// returns it once Start has opened it; b must not be started yet.
func useOwnedSocket(t *testing.T, b *Bridge) func() *ownedSocket {
	t.Helper()
	var sock atomic.Pointer[ownedSocket]
	open := func() (*udp.Listener, error) {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return nil, err
		}
		s := &ownedSocket{UDPConn: conn}
		sock.Store(s)
		l, _ := udpseam.NewListener(s).(*udp.Listener)
		return l, nil
	}
	b.openUDP.Store(&open)
	return func() *ownedSocket {
		s := sock.Load()
		if s == nil {
			t.Fatal("the bridge did not open its socket through the seam")
		}
		return s
	}
}
