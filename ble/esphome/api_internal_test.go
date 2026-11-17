// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"

	"github.com/SukramJ/go-fabric/ble/esphome/internal/wire"
)

// stalledConn is an API connection whose peer reads nothing: every write
// blocks until the write deadline or the close.
func stalledConn(t *testing.T, clk clock.Clock, writeTimeout time.Duration) *apiConn {
	t.Helper()
	nc, peer := net.Pipe()
	t.Cleanup(func() { _ = nc.Close(); _ = peer.Close() })
	a := &apiConn{nc: nc, f: wire.NewPlaintext(nc), clk: clk, log: slog.New(slog.DiscardHandler), done: make(chan struct{}), writeTimeout: writeTimeout}
	a.lastRx.Store(clk.Now().UnixNano())
	return a
}

// TestStalledPeerEndsTheConnection (R2-P6-err-2010): a peer that stops
// reading does not hold the connection. A write past writeTimeout fails
// and closes it, and the keep-alive's silence check runs on while a ping
// is stuck in the write.
func TestStalledPeerEndsTheConnection(t *testing.T) {
	t.Run("write deadline", func(t *testing.T) {
		a := stalledConn(t, clock.System{}, 50*time.Millisecond)
		sent := make(chan error, 1)
		go func() { sent <- a.send(wire.Empty(wire.TypePingRequest)) }()
		select {
		case err := <-sent:
			if !errors.Is(err, ErrProxyUnavailable) {
				t.Fatalf("send = %v, want ErrProxyUnavailable", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("send still blocked on a peer that reads nothing")
		}
		select {
		case <-a.done:
		default:
			t.Fatal("the connection outlived the failed write")
		}
	})
	t.Run("silence check beside a stuck ping", func(t *testing.T) {
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		a := stalledConn(t, clk, time.Hour)
		go a.keepAlive()
		for range 20 {
			clk.Advance(pingInterval)
			clk.Yield()
			select {
			case <-a.done:
				if !errors.Is(a.cause(), errPongTimeout) {
					t.Fatalf("closed with %v, want errPongTimeout", a.cause())
				}
				return
			default:
			}
		}
		t.Fatalf("no close after %v of silence with a ping stuck in the write", 20*pingInterval)
	})
}
