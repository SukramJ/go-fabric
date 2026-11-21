// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/tcp"
)

// TestTCPCloseEvictsSessionBoundByHandlerInFlight: a handler that passed
// the reader's closed check before the connection closed and binds a
// session after the close-time eviction ran leaves no route behind — the
// session it bound is evicted once the handler returned (review II,
// R2-P6-conc-0306, the TCP half). matter.js delivers nothing on a closed
// channel and evicts every session whose transport channel is the closed
// one (TcpChannel.ts:196-198; ExchangeManager.ts:707-722, :724-740 at
// b25ed960).
//
// The listener is the test's own, so the handler can be parked across the
// close; behind the park run the bridge's own receive steps (routeFor,
// then dispatch — conn.go receive), and the close path is the bridge's
// handleTCPClose. The bridge closes the connection itself, as its idle
// timer, its last-session close or Stop do: a close the peer causes is
// held until the message in dispatch released its turn (transport/tcp
// conn.go awaitDelivery). Each case takes a route before it parks — the
// route an earlier message on the connection left — and its removal is
// how the test sees the close-time eviction ran before the handler goes on.
func TestTCPCloseEvictsSessionBoundByHandlerInFlight(t *testing.T) {
	t.Run("route taken before the close, session bound after it", func(t *testing.T) {
		testTCPCloseInFlight(t, func(b *Bridge, c *tcp.Conn, route net.Addr, msg []byte, release func()) {
			ctx := context.WithValue(b.handlerContext(), sessionTurnKey{}, release)
			_ = b.dispatch(ctx, msg, route)
		})
	})
	t.Run("route taken after the close", func(t *testing.T) {
		testTCPCloseInFlight(t, func(b *Bridge, c *tcp.Conn, _ net.Addr, msg []byte, release func()) {
			b.handleTCPMessage(c, msg, release)
		})
	})
}

// testTCPCloseInFlight parks the handler of one InvokeRequest across the
// connection's close and then runs resume, which serves it.
func testTCPCloseInFlight(t *testing.T, resume func(b *Bridge, c *tcp.Conn, route net.Addr, msg []byte, release func())) {
	t.Helper()
	h := newTCPHarness(t)
	b := h.bridge

	l, err := tcp.Listen(tcp.Config{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	parked := make(chan *tcp.Conn, 1)
	unpark := make(chan struct{})
	var unparkOnce sync.Once
	handled := make(chan struct{})
	handler := func(c *tcp.Conn, msg []byte, release func()) {
		route := b.routeFor(c)
		parked <- c
		<-unpark
		resume(b, c, route, msg, release)
		close(handled)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, handler, b.handleTCPClose)
	}()
	t.Cleanup(func() {
		cancel()
		unparkOnce.Do(func() { close(unpark) })
		<-served
	})

	var d tcp.Dialer
	client, err := d.Dial(context.Background(), l.Addr().String(), func(*tcp.Conn, []byte, func()) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Send(h.sealedInvoke(0x311)); err != nil {
		t.Fatal(err)
	}
	var server *tcp.Conn
	select {
	case server = <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never ran")
	}

	_ = server.Close()
	waitFor(t, "the close-time eviction", func() bool {
		_, ok := b.connRoutes.Load(server)
		return !ok
	})

	// The handler goes on: its message binds the session to a route of
	// the closed connection.
	unparkOnce.Do(func() { close(unpark) })
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never returned")
	}

	waitFor(t, "the session bound after the close evicted", func() bool {
		h.registry.mu.Lock()
		defer h.registry.mu.Unlock()
		return len(h.registry.closed) == 1 && h.registry.closed[0] == harnessLocalSessionID
	})
	if _, ok := b.sessionPeerAddrs.Load(harnessLocalSessionID); ok {
		t.Fatal("evicted session still has a route")
	}
	if _, ok := b.connRoutes.Load(server); ok {
		t.Fatal("the closed connection's route outlived the close")
	}
}
