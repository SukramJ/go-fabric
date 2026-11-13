// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestConnWaitReturnsForAConnClosedBeforeItsReaderRan pins the sentence in
// Conn.Wait's doc (review II, W1-conc-07): a connection that is closed
// before serve started has no reader and no handler to wait for, so Wait
// returns nil at once. Before, served was closed by serve alone and Wait
// blocked until its context ended. A serve that starts after the close
// returns without a second close of served.
func TestConnWaitReturnsForAConnClosedBeforeItsReaderRan(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	c := newConn(tcpChannelPipe{b, &net.TCPAddr{IP: net.ParseIP("::1"), Port: 5540}}, 0, true, nil, nil)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// The bound is wall time only when Wait does not return at once.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Wait(ctx); err != nil {
		t.Fatalf("Wait on a connection closed before its reader ran = %v, want nil at once", err)
	}
	c.serve(func(*Conn, []byte, func()) { t.Error("a handler ran on a closed connection") })
	if err := c.Wait(ctx); err != nil {
		t.Fatalf("Wait after a late serve = %v, want nil", err)
	}
}

// TestListenerWaitIdleWaitsForTheHandlers pins the owners of a TCP
// connection's goroutines (G1-14, ADR 0026 P5): the reader returns only
// after every handler call it started, Conn.Wait waits for it, and the
// listener's WaitIdle waits for every accepted connection — bounded by
// its context while a handler still runs. Close itself does not wait, so
// a handler may close its own connection.
func TestListenerWaitIdleWaitsForTheHandlers(t *testing.T) {
	release := make(chan struct{})
	got := make(chan *Conn, 1)
	var returned atomic.Bool
	l, closed := startListener(t, func(c *Conn, _ []byte, _ func()) {
		got <- c
		<-release
		returned.Store(true)
	}, 0)
	c := dialRaw(t, l)
	if _, err := c.Write(frameOf([]byte{1})); err != nil {
		t.Fatal(err)
	}
	conn := <-got
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	<-closed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.WaitIdle(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitIdle with a handler running = %v, want context.Canceled", err)
	}
	if err := conn.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Conn.Wait with a handler running = %v, want context.Canceled", err)
	}
	close(release)
	if err := conn.Wait(context.Background()); err != nil {
		t.Fatalf("Conn.Wait: %v", err)
	}
	if !returned.Load() {
		t.Fatal("Conn.Wait returned before the handler did")
	}
	if err := l.WaitIdle(context.Background()); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
}

// TestDialedConnWait: the dialer's caller owns the connection; Close and
// Wait leave nothing running.
func TestDialedConnWait(t *testing.T) {
	l, _ := startListener(t, func(*Conn, []byte, func()) {}, 0)
	var d Dialer
	conn, err := d.Dial(context.Background(), l.Addr().String(), func(*Conn, []byte, func()) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// panickingReadConn is a connection whose Read panics.
type panickingReadConn struct{ tcpChannelPipe }

func (panickingReadConn) Read([]byte) (int, error) { panic("read boom") }

// TestConnServeRecoversAReaderPanic (review II, R2-P1-req-02, defence in
// depth): a panic in the connection's read loop — the framing of
// untrusted bytes runs there — closes that connection and is logged and
// counted like a handler panic, instead of ending the process from a
// goroutine nothing recovers.
func TestConnServeRecoversAReaderPanic(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	var panics panicCounter
	var hooked atomic.Int32
	panics.set(func() { hooked.Add(1) })
	nc := panickingReadConn{tcpChannelPipe{b, &net.TCPAddr{IP: net.ParseIP("::1"), Port: 5540}}}
	c := newConn(nc, 0, true, nil, &panics)
	go c.serve(func(*Conn, []byte, func()) {})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Wait(ctx); err != nil {
		t.Fatalf("Wait after a reader panic: %v", err)
	}
	if !c.isClosed() {
		t.Error("connection still open after its reader panicked")
	}
	if got := panics.n.Load(); got != 1 || hooked.Load() != 1 {
		t.Errorf("recovered = %d, hook ran %d times, want 1 and 1", got, hooked.Load())
	}
}
