// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A port of matter.js packages/protocol/test/protocol/TcpSessionBindingTest.ts
// at 862ad75f: sessions bound to a TCP connection, the eviction when it
// drops, its log lines, the connection's lifecycle and the channel type.
//
// The Go surface differs: the bridge is the ExchangeManager, a session's
// transportChannel is its route in sessionPeerAddrs (a *connRoute per
// connection), Session.initiateForceClose on a drop is evictSession (the
// registry's peer-lost Close), and the environment's log origin is the
// bridge's own logger.

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// tcpSessionBindingChannel is MockTcpChannel: reliable, named, records
// its close.
type tcpSessionBindingChannel struct {
	name   string
	mu     sync.Mutex
	closed bool
}

func (c *tcpSessionBindingChannel) Send([]byte) error { return nil }
func (c *tcpSessionBindingChannel) Remote() net.Addr {
	return &net.TCPAddr{IP: net.IPv6loopback, Port: 5540}
}
func (c *tcpSessionBindingChannel) Reliable() bool { return true }
func (c *tcpSessionBindingChannel) String() string { return "tcp://" + c.name }
func (c *tcpSessionBindingChannel) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *tcpSessionBindingChannel) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// tcpSessionBindingLog records the messages of the lines it receives.
type tcpSessionBindingLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *tcpSessionBindingLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *tcpSessionBindingLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.msgs = append(l.msgs, r.Message)
	l.mu.Unlock()
	return nil
}
func (l *tcpSessionBindingLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *tcpSessionBindingLog) WithGroup(string) slog.Handler      { return l }

func (l *tcpSessionBindingLog) has(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

// tcpSessionBindingBridge is an unstarted bridge with a recording
// session registry; logger nil leaves New's default.
func tcpSessionBindingBridge(t *testing.T, logger *slog.Logger) (*Bridge, *fakeSessionRegistry) {
	t.Helper()
	b, err := New(wbEmptySnapshotter, newFakeAdvertiser(), Config{Listen: ":0"}, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reg := &fakeSessionRegistry{}
	b.AttachSessionRegistry(reg)
	return b, reg
}

// tcpSessionBindingSession binds session sid to ch, as the first
// authenticated message on it does; returns ch's route.
func tcpSessionBindingSession(b *Bridge, ch *tcpSessionBindingChannel, sid uint16) *connRoute {
	r, _ := b.routeFor(ch).(*connRoute)
	b.bindSessionRoute(sid, r)
	return r
}

// transportChannel is the connection session sid is bound to.
func tcpSessionBindingChannelOf(b *Bridge, sid uint16) any {
	raw, ok := b.sessionPeerAddrs.Load(sid)
	if !ok {
		return nil
	}
	if r, isConn := raw.(*connRoute); isConn {
		return r.conn
	}
	return raw
}

func tcpSessionBindingClosed(reg *fakeSessionRegistry) []uint16 {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return append([]uint16(nil), reg.closed...)
}

func TestTcpSessionBindingConnectionDropEvictsSessions(t *testing.T) {
	t.Run("finds sessions by channel identity", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		tcpSessionBindingSession(b, ch, 1)
		if got := tcpSessionBindingChannelOf(b, 1); got != ch {
			t.Fatalf("session 1 bound to %v, want the channel itself", got)
		}
	})
	t.Run("sessions on different connections are distinguishable", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		ch1 := &tcpSessionBindingChannel{name: "tcp-1"}
		ch2 := &tcpSessionBindingChannel{name: "tcp-2"}
		r1 := tcpSessionBindingSession(b, ch1, 1)
		r2 := tcpSessionBindingSession(b, ch2, 2)
		if tcpSessionBindingChannelOf(b, 1) != ch1 || tcpSessionBindingChannelOf(b, 2) != ch2 || r1 == r2 {
			t.Fatal("sessions on two connections share a binding")
		}
	})
	t.Run("session force-close marks session as closing", func(t *testing.T) {
		b, reg := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		r := tcpSessionBindingSession(b, ch, 1)
		if len(tcpSessionBindingClosed(reg)) != 0 {
			t.Fatal("session closing before the force close")
		}
		b.evictSession(1, r)
		if got := tcpSessionBindingClosed(reg); len(got) != 1 || got[0] != 1 {
			t.Fatalf("registry closed %v, want [1]", got)
		}
	})
	t.Run("exchange can be closed with error", func(t *testing.T) {
		// The session's exchange state goes with the drop
		// (ExchangeManager.ts:660-662).
		b, _ := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		r := tcpSessionBindingSession(b, ch, 1)
		key := mrp.ExchangeKey{SessionID: 1, ExchangeID: 1, Initiator: true}
		b.routing.exchangeSrcs.Store(key, exchangeReplyTarget{src: r, sessionID: 1})
		b.evictSession(1, r)
		if _, ok := b.routing.exchangeSrcs.Load(key); ok {
			t.Fatal("exchange of the evicted session still present")
		}
	})
}

func TestTcpSessionBindingManagerReaction(t *testing.T) {
	t.Run("names the originating environment on the lines the drop produces", func(t *testing.T) {
		log := &tcpSessionBindingLog{}
		b, _ := tcpSessionBindingBridge(t, slog.New(log))
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		tcpSessionBindingSession(b, ch, 1)
		b.evictConn(ch)
		// "TCP connection dropped" (ExchangeManager.ts:651) and "Evicting
		// session due to TCP disconnect" (:655), on the node's own logger.
		for _, line := range []string{"matter.tcp.evict", "matter.tcp.evict.session"} {
			if !log.has(line) {
				t.Errorf("line %q not on the bridge's logger", line)
			}
		}
	})
	t.Run("names no origin when the manager is built without one", func(t *testing.T) {
		log := &tcpSessionBindingLog{}
		prev := slog.Default()
		slog.SetDefault(slog.New(log))
		t.Cleanup(func() { slog.SetDefault(prev) })
		b, _ := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-unbound"}
		tcpSessionBindingSession(b, ch, 1)
		b.evictConn(ch)
		if !log.has("matter.tcp.evict") {
			t.Fatal("a bridge built without a logger logged the drop nowhere")
		}
	})
	t.Run("leaves a session on another connection alone", func(t *testing.T) {
		b, reg := tcpSessionBindingBridge(t, nil)
		dropped := &tcpSessionBindingChannel{name: "tcp-1"}
		surviving := &tcpSessionBindingChannel{name: "tcp-2"}
		tcpSessionBindingSession(b, dropped, 1)
		tcpSessionBindingSession(b, surviving, 2)
		b.evictConn(dropped)
		if got := tcpSessionBindingClosed(reg); len(got) != 1 || got[0] != 1 {
			t.Fatalf("registry closed %v, want [1]", got)
		}
		if tcpSessionBindingChannelOf(b, 2) != surviving {
			t.Fatal("the session on the other connection lost its binding")
		}
	})
}

func TestTcpSessionBindingConnectionLifecycle(t *testing.T) {
	t.Run("TCP connection is NOT closed by MessageChannel.close()", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		r := tcpSessionBindingSession(b, ch, 1)
		b.evictSession(1, r)
		if ch.isClosed() {
			t.Fatal("force-closing the session closed the connection")
		}
	})
	t.Run("multiple sessions can share the same TCP connection", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		ch := &tcpSessionBindingChannel{name: "tcp-1"}
		r := tcpSessionBindingSession(b, ch, 1)
		tcpSessionBindingSession(b, ch, 2)
		b.routeBindMu.Lock()
		n := r.sessions
		b.routeBindMu.Unlock()
		if tcpSessionBindingChannelOf(b, 1) != ch || tcpSessionBindingChannelOf(b, 2) != ch || n != 2 || ch.isClosed() {
			t.Fatalf("shared binding: sessions=%d closed=%v", n, ch.isClosed())
		}
	})
	t.Run("closing one session does not affect sessions on other connections", func(t *testing.T) {
		b, reg := tcpSessionBindingBridge(t, nil)
		ch1 := &tcpSessionBindingChannel{name: "tcp-1"}
		ch2 := &tcpSessionBindingChannel{name: "tcp-2"}
		r1 := tcpSessionBindingSession(b, ch1, 1)
		tcpSessionBindingSession(b, ch2, 2)
		b.evictSession(1, r1)
		if got := tcpSessionBindingClosed(reg); len(got) != 1 || got[0] != 1 {
			t.Fatalf("registry closed %v, want [1]", got)
		}
		if tcpSessionBindingChannelOf(b, 2) != ch2 || ch1.isClosed() || ch2.isClosed() {
			t.Fatal("closing session 1 touched the other connection")
		}
	})
}

func TestTcpSessionBindingChannelTypeDetection(t *testing.T) {
	t.Run("TCP channels have type TCP", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		r := b.routeFor(&tcpSessionBindingChannel{name: "tcp-1"})
		if !isConnRoute(r) || r.Network() != "tcp" || routeUsesMRP(r) {
			t.Fatalf("route %T network=%s mrp=%v", r, r.Network(), routeUsesMRP(r))
		}
	})
	t.Run("UDP channels have type UDP", func(t *testing.T) {
		b, _ := tcpSessionBindingBridge(t, nil)
		var l udp.Listener
		r := b.routeFor(l.Conn(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}))
		if isConnRoute(r) || r.Network() != "udp" || !routeUsesMRP(r) {
			t.Fatalf("route %T network=%s mrp=%v", r, r.Network(), routeUsesMRP(r))
		}
	})
}
