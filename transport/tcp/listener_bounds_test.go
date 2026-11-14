// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

// The listener's total bound on inbound connections and its retry of a
// failed accept (review II, R2-P3-ctl-lan-03, R2-P3-ctl-lan-04,
// R2-P3-lan-dev-05).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// pipeFrom is one end of an in-memory connection whose remote address is
// ip — the way a test presents many source addresses to accept without
// the kernel.
type pipeFrom struct {
	net.Conn
	remote net.Addr
}

func (c pipeFrom) RemoteAddr() net.Addr { return c.remote }

// acceptFrom hands the listener one in-memory connection from the i-th
// address and returns its listener-side Conn (nil when it was not
// registered) and the client end.
func acceptFrom(t *testing.T, l *Listener, i int, handler Handler) (registered *Conn, client net.Conn) {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { _ = cli.Close() })
	before := l.connSet()
	l.accept(pipeFrom{Conn: srv, remote: &net.TCPAddr{IP: net.ParseIP(fmt.Sprintf("fd00::%x", i+1)), Port: 40000}}, handler, nil)
	for c := range l.connSet() {
		if !before[c] {
			return c, cli
		}
	}
	return nil, cli
}

func (l *Listener) connSet() map[*Conn]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[*Conn]bool, len(l.conns))
	for c := range l.conns {
		out[c] = true
	}
	return out
}

// TestListenerHoldsAtMostMaxInboundConnections: whatever the number of
// source addresses, the listener holds at most MaxInboundConnections
// inbound connections; once each has delivered a message, a further one
// is closed and the ones held stay open.
func TestListenerHoldsAtMostMaxInboundConnections(t *testing.T) {
	t.Parallel()
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	got := make(chan struct{}, 1024)
	handler := func(_ *Conn, _ []byte, release func()) { release(); got <- struct{}{} }

	held := make([]*Conn, 0, MaxInboundConnections)
	for i := range MaxInboundConnections {
		c, cli := acceptFrom(t, l, i, handler)
		if c == nil {
			t.Fatalf("connection %d of %d was not held", i+1, MaxInboundConnections)
		}
		held = append(held, c)
		if _, err := cli.Write(frameOf([]byte{1})); err != nil {
			t.Fatal(err)
		}
		<-got
	}
	const addrs = 200
	for i := MaxInboundConnections; i < addrs; i++ {
		for range MaxConnectionsPerPeerIP {
			c, cli := acceptFrom(t, l, i, handler)
			if c != nil {
				t.Fatalf("the listener holds %d connections and took one more from address %d", MaxInboundConnections, i)
			}
			_, _ = cli.Write(frameOf([]byte{1})) // refused: the pipe is closed
		}
	}
	if n := l.Conns(); n != MaxInboundConnections {
		t.Fatalf("the listener holds %d inbound connections from %d addresses, want %d", n, addrs, MaxInboundConnections)
	}
	for i, c := range held {
		select {
		case <-c.Closed():
			t.Errorf("held connection %d closed: %v", i, c.Err())
		default:
		}
	}
}

// TestListenerReclaimsAConnectionWithoutAMessageWhenFull: at the cap, a
// new connection takes the place of the oldest one that has delivered no
// message yet.
func TestListenerReclaimsAConnectionWithoutAMessageWhenFull(t *testing.T) {
	t.Parallel()
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	got := make(chan struct{}, 16)
	handler := func(_ *Conn, _ []byte, release func()) { release(); got <- struct{}{} }

	var silent []*Conn
	for i := range MaxInboundConnections {
		c, cli := acceptFrom(t, l, i, handler)
		if c == nil {
			t.Fatalf("connection %d was not held", i+1)
		}
		if i == 0 { // the oldest delivers a message: it is not reclaimed
			if _, err := cli.Write(frameOf([]byte{1})); err != nil {
				t.Fatal(err)
			}
			<-got
			continue
		}
		silent = append(silent, c)
	}
	c, _ := acceptFrom(t, l, MaxInboundConnections, handler)
	if c == nil {
		t.Fatal("a new connection was refused while a held one had delivered no message")
	}
	select {
	case <-silent[0].Closed():
		if !errors.Is(silent[0].Err(), errReclaimed) {
			t.Errorf("the reclaimed connection closed with %v, want errReclaimed", silent[0].Err())
		}
	default:
		t.Fatal("the oldest connection without a message is still open")
	}
	if n := l.Conns(); n != MaxInboundConnections {
		t.Errorf("conns = %d, want %d", n, MaxInboundConnections)
	}
}

// scriptedListener is a net.Listener whose first Accepts fail with errs,
// then hand out the connections sent on conns until Close.
type scriptedListener struct {
	mu     sync.Mutex
	errs   []error
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (s *scriptedListener) Accept() (net.Conn, error) {
	s.mu.Lock()
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	select {
	case c := <-s.conns:
		return c, nil
	case <-s.closed:
		return nil, net.ErrClosed
	}
}

func (s *scriptedListener) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *scriptedListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv6loopback, Port: MatterPort}
}

// TestServeRetriesAFailedAccept: an accept that fails with the process
// out of descriptors does not end Serve — it waits 5 ms, doubling to 1 s
// (net/http's Server.Serve), and accepts again; it returns nil when its
// context ends.
func TestServeRetriesAFailedAccept(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		emfile := &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", syscall.EMFILE)}
		const failures = 10
		s := &scriptedListener{conns: make(chan net.Conn), closed: make(chan struct{})}
		for range failures {
			s.errs = append(s.errs, emfile)
		}
		l := newListener(s, Config{MaxPayloadSize: 64})
		got := make(chan []byte, 1)
		ctx, cancel := context.WithCancel(t.Context())
		served := make(chan error, 1)
		start := time.Now()
		go func() {
			served <- l.Serve(ctx, func(_ *Conn, msg []byte, release func()) { release(); got <- msg }, nil)
		}()

		srv, cli := net.Pipe()
		defer func() { _ = cli.Close() }()
		select {
		case s.conns <- srv:
		case err := <-served:
			t.Fatalf("Serve returned %v after an accept failed with the descriptors exhausted; want it to accept again", err)
		}
		// 5, 10, 20, …, 640 ms, then 1 s twice.
		want := time.Duration(0)
		for d, i := acceptRetryFirst, 0; i < failures; i++ {
			want += d
			d = min(2*d, acceptRetryMax)
		}
		if elapsed := time.Since(start); elapsed != want {
			t.Errorf("the accept came back after %v, want %v", elapsed, want)
		}
		if _, err := cli.Write(frameOf([]byte{7})); err != nil {
			t.Fatal(err)
		}
		if msg := <-got; len(msg) != 1 || msg[0] != 7 {
			t.Fatalf("message %v", msg)
		}
		cancel()
		if err := <-served; err != nil {
			t.Errorf("Serve after its context ended = %v, want nil", err)
		}
	})
}
