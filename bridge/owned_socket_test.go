// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/internal/udpseam"
	"thingfabric.dev/go-fabric/transport/udp"
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
// bridge writes, records the close, and counts the datagrams read and
// those whose handler returned (udpseam.Handled).
type ownedSocket struct {
	*net.UDPConn
	writes  atomic.Int32
	closed  atomic.Bool
	reads   atomic.Int64
	handled atomic.Int64
	settled chan struct{} // a datagram's handler returned
}

func (s *ownedSocket) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	n, addr, err := s.UDPConn.ReadFromUDP(b)
	if err == nil {
		s.reads.Add(1)
	}
	return n, addr, err
}

// Handled implements udpseam.Handled: the listener calls it once the
// handler of a datagram it read returned, or it dropped the datagram.
func (s *ownedSocket) Handled() {
	s.handled.Add(1)
	select {
	case s.settled <- struct{}{}:
	default:
	}
}

// waitHandled waits until the handler of every datagram the socket has
// read so far returned.
func (s *ownedSocket) waitHandled(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for s.handled.Load() < s.reads.Load() {
		select {
		case <-s.settled:
		case <-deadline.C:
			t.Fatalf("%d of %d datagrams read are still being handled", s.reads.Load()-s.handled.Load(), s.reads.Load())
		}
	}
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
		s := &ownedSocket{UDPConn: conn, settled: make(chan struct{}, 1)}
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
