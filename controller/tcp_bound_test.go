// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestControllerTCPListenerHoldsTheConfiguredInboundBound: a controller
// whose TCP listener is bounded at one connection gives a second
// connection the place of the first, which has delivered no message and
// is past the age that spares a fresh connection (a fraction of
// tcp.NewConnectionIdleTimeout, review II R2-P6-bound-0203); a connection
// that arrives while the first is younger is refused.
func TestControllerTCPListenerHoldsTheConfiguredInboundBound(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	cfg := config(openDB(t))
	cfg.Clock = fake
	cfg.TCP = controller.TCPConfig{Incoming: true, MaxInboundConnections: 1}
	c, err := controller.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var d tcp.Dialer
	dial := func() *tcp.Conn {
		t.Helper()
		conn, err := d.Dial(ctx, c.TCPAddr().String(), func(*tcp.Conn, []byte, func()) {})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	first, early := dial(), dial()
	// The early one is refused: the first, held, is younger than the floor.
	select {
	case <-early.Closed():
	case <-ctx.Done():
		t.Fatal("a connection took the place of one younger than the floor")
	}
	fake.Advance(tcp.NewConnectionIdleTimeout / 2)
	second := dial()
	select {
	case <-first.Closed():
	case <-ctx.Done():
		t.Fatal("the first connection is still open beside a second under a bound of one")
	}
	select {
	case <-second.Closed():
		t.Fatalf("the second connection closed: %v", second.Err())
	default:
	}
}

// TestTCPMaxInboundConnectionsNegativeIsRefused: a negative bound is a
// configuration error.
func TestTCPMaxInboundConnectionsNegativeIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config(openDB(t))
	cfg.TCP = controller.TCPConfig{Incoming: true, MaxInboundConnections: -1}
	if c, err := controller.New(t.Context(), cfg); !errors.Is(err, controller.ErrConfiguration) {
		if err == nil {
			_ = c.Close(t.Context())
		}
		t.Fatalf("New with a negative TCP.MaxInboundConnections returned %v, want ErrConfiguration", err)
	}
}

// takenTCPPorts makes the TCP binds New attempts fail as a port taken
// for TCP (EADDRINUSE) — the first taken of them, or every one with
// taken < 0 — and records the addresses they asked for.
type takenTCPPorts struct {
	taken int
	mu    sync.Mutex
	asked []string
}

func (p *takenTCPPorts) listen(cfg tcp.Config) (*tcp.Listener, error) {
	p.mu.Lock()
	p.asked = append(p.asked, cfg.LocalAddr)
	n := len(p.asked)
	p.mu.Unlock()
	if p.taken < 0 || n <= p.taken {
		return nil, fmt.Errorf("tcp: listen %s: %w", cfg.LocalAddr, syscall.EADDRINUSE)
	}
	return tcp.Listen(cfg)
}

func (p *takenTCPPorts) attempts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.asked...)
}

// TestControllerRetriesAnAutoAssignedPortTakenForTCP (review II,
// R2-P5-tassert-02): with TCP incoming, an auto-assigned port that is
// taken for TCP is given up and the UDP and TCP pair bound again, as
// matter.js's ServerNetworkRuntime.ts:212-231 does (b25ed960); a port
// the host chose — here a socket it gave — is tried once, and an
// auto-assigned one at most ten times. Not parallel: the TCP constructor
// it replaces is the package's.
func TestControllerRetriesAnAutoAssignedPortTakenForTCP(t *testing.T) {
	t.Run("taken once", func(t *testing.T) {
		ports := &takenTCPPorts{taken: 1}
		defer controller.SetTCPListen(ports.listen)()
		cfg := config(openDB(t))
		cfg.TCP = controller.TCPConfig{Incoming: true}
		c, err := controller.New(t.Context(), cfg)
		if err != nil {
			t.Fatalf("New with the first auto-assigned port taken for TCP: %v (TCP binds %q)", err, ports.attempts())
		}
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		// Two binds, or more when the next pair's port was taken for TCP
		// too — as it was in 1 of 600 runs under four test processes.
		if got := ports.attempts(); len(got) < 2 {
			t.Fatalf("TCP binds %q, want the taken port's and the next pair's", got)
		}
		if c.TCPAddr().Port != c.Addr().Port {
			t.Fatalf("TCP on %v beside UDP on %v, want one port", c.TCPAddr(), c.Addr())
		}
	})
	t.Run("always taken", func(t *testing.T) {
		ports := &takenTCPPorts{taken: -1}
		defer controller.SetTCPListen(ports.listen)()
		cfg := config(openDB(t))
		cfg.TCP = controller.TCPConfig{Incoming: true}
		if c, err := controller.New(t.Context(), cfg); !errors.Is(err, syscall.EADDRINUSE) {
			if err == nil {
				_ = c.Close(t.Context())
			}
			t.Fatalf("New with every auto-assigned port taken for TCP: %v, want EADDRINUSE", err)
		}
		if got := ports.attempts(); len(got) != 10 {
			t.Fatalf("%d TCP binds, want 10 (MAX_PORT_ASSIGNMENT_RETRIES): %q", len(got), got)
		}
	})
	t.Run("given socket", func(t *testing.T) {
		ports := &takenTCPPorts{taken: -1}
		defer controller.SetTCPListen(ports.listen)()
		l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0"})
		if err != nil {
			t.Fatal(err)
		}
		cfg := config(openDB(t))
		cfg.Listener = l
		cfg.TCP = controller.TCPConfig{Incoming: true}
		if c, err := controller.New(t.Context(), cfg); !errors.Is(err, syscall.EADDRINUSE) {
			if err == nil {
				_ = c.Close(t.Context())
			}
			t.Fatalf("New on a given socket whose port is taken for TCP: %v, want EADDRINUSE", err)
		}
		if got := ports.attempts(); len(got) != 1 {
			t.Fatalf("TCP binds %q on a given socket, want one", got)
		}
		// A New that fails closes the socket it was given
		// (controller.go Config.Listener, New's deferred Close; R2-P5
		// DR5a-code-02): its address binds again. A socket still open
		// holds it, and the bind fails with EADDRINUSE.
		again, err := net.ListenUDP("udp", l.LocalAddr())
		if err != nil {
			_ = l.Close()
			t.Fatalf("the given socket's address after the failed New: %v, want it closed and free", err)
		}
		_ = again.Close()
	})
}
