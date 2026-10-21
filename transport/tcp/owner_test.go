// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

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
