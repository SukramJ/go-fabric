// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// slowCloseConn is a socket whose Close unblocks its readers first and
// releases its address only when release is closed — the order of a
// kernel socket (the reader returns on the evict, the descriptor goes
// after) and of fabrictest's in-memory socket (done closed, then the
// address key released).
type slowCloseConn struct {
	readers  chan struct{}
	entered  chan struct{}
	release  chan struct{}
	released atomic.Bool
}

func newSlowCloseConn() *slowCloseConn {
	return &slowCloseConn{readers: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
}

func (c *slowCloseConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) {
	<-c.readers
	return 0, nil, net.ErrClosed
}

func (c *slowCloseConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }

func (c *slowCloseConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv6loopback, Port: MatterPort}
}

func (c *slowCloseConn) Close() error {
	close(c.readers)
	close(c.entered)
	<-c.release
	c.released.Store(true)
	return nil
}

// TestCloseWaitsForTheClosingCall pins the restart flake's fix (G1-15):
// Serve's cancel closes the socket from its watcher goroutine, and a host
// shutting down calls Close after Serve returned. Neither Serve nor the
// second Close may return before the socket's Close has — the next
// listener on the address (fabrictest's Controller.Restart) would find
// it still taken.
func TestCloseWaitsForTheClosingCall(t *testing.T) {
	t.Parallel()
	conn := newSlowCloseConn()
	l := newListener(conn)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan bool)
	go func() {
		_ = l.Serve(ctx, func([]byte, *net.UDPAddr) {})
		served <- conn.released.Load()
	}()
	cancel()
	<-conn.entered // the watcher is inside the socket's Close
	closed := make(chan bool)
	go func() {
		_ = l.Close()
		closed <- conn.released.Load()
	}()
	// Released late: a Close or a Serve that does not wait has returned
	// by then and reported the socket still holding its address.
	time.AfterFunc(50*time.Millisecond, func() { close(conn.release) })
	if !<-served {
		t.Error("Serve returned before the socket's Close did")
	}
	if !<-closed {
		t.Error("the second Close returned before the socket's Close did")
	}
}

// TestCloseReturnsTheSocketErrorOnce: the Close that closed the socket
// reports its error, a later one nil.
func TestCloseReturnsTheSocketErrorOnce(t *testing.T) {
	t.Parallel()
	conn := newSlowCloseConn()
	close(conn.release)
	l := newListener(conn)
	if err := l.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !l.isClosed() {
		t.Fatal("the listener does not report closed")
	}
}
