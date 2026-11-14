// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// TestControllerTCPListenerHoldsTheConfiguredInboundBound: a controller
// whose TCP listener is bounded at one connection gives a second
// connection the place of the first, which has delivered no message.
func TestControllerTCPListenerHoldsTheConfiguredInboundBound(t *testing.T) {
	t.Parallel()
	cfg := config(openDB(t))
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
	first, second := dial(), dial()
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
