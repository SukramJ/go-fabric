// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// TestSendToAPeerThatStopsReadingIsBounded holds R2-P6-bound-0202: a Send
// to a peer that reads nothing fails once the write made no progress for
// the connection's send timeout, and the connection is closed — a stuck
// peer costs its connection, not the sending goroutine and every Send
// queued behind it.
func TestSendToAPeerThatStopsReadingIsBounded(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newListener(ln, Config{})
	defer l.Close()
	type outcome struct {
		err  error
		took time.Duration
		conn *Conn
	}
	done := make(chan outcome, 1)
	go func() {
		_ = l.Serve(context.Background(), func(c *Conn, _ []byte, _ func()) {
			c.sendTimeout = 300 * time.Millisecond
			big := make([]byte, DefaultMaxPayloadSize)
			start := time.Now()
			var err error
			for range 1000 { // 64 MB: far beyond any socket buffer
				if err = c.Send(big); err != nil {
					break
				}
			}
			done <- outcome{err, time.Since(start), c}
		}, nil)
	}()
	peer, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if _, err := peer.Write([]byte{1, 0, 0, 0, 0x42}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	select {
	case o := <-done:
		if !errors.Is(o.err, os.ErrDeadlineExceeded) {
			t.Fatalf("Send returned %v, want a deadline error", o.err)
		}
		select {
		case <-o.conn.Closed():
		default:
			t.Fatal("the connection is still open after the send timed out")
		}
		if err := o.conn.Send([]byte{1}); !errors.Is(err, ErrConnClosed) {
			t.Fatalf("Send after the timeout returned %v, want ErrConnClosed", err)
		}
	case <-ctx.Done():
		t.Fatal("Send still blocked: no write deadline applies")
	}
}
