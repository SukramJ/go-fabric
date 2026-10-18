// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

// A port of matter.js packages/protocol/test/transport/tcp/TcpTransportTest.ts
// at 862ad75f: the disconnect notification and closing the transport.
//
// The Go surface differs: TcpTransport's server role is Listener (its
// onDisconnect is Serve's onClose), its client role is Dialer, and the
// NetworkSimulator hosts are loopback sockets.

import (
	"context"
	"errors"
	"testing"
	"time"
)

func tcpTransportDial(t *testing.T, l *Listener) *Conn {
	t.Helper()
	var d Dialer
	c, err := d.Dial(context.Background(), l.Addr().String(), func(*Conn, []byte, func()) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestTcpTransportOnDisconnect(t *testing.T) {
	t.Run("fires when a connection drops", func(t *testing.T) {
		got := make(chan *Conn, 1)
		l, disconnected := startListener(t, func(c *Conn, _ []byte, _ func()) { got <- c }, 0)
		channel := tcpTransportDial(t, l)
		// A first message lets the server side see the connection.
		if err := channel.Send([]byte{1}); err != nil {
			t.Fatal(err)
		}
		server := <-got
		_ = channel.Close()
		select {
		case c := <-disconnected:
			if c != server {
				t.Fatal("disconnect reported another connection")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no disconnect")
		}
		select {
		case c := <-disconnected:
			t.Fatalf("second disconnect %v", c)
		default:
		}
	})
}

func TestTcpTransportClose(t *testing.T) {
	t.Run("closes server and all connections", func(t *testing.T) {
		got := make(chan *Conn, 1)
		l, _ := startListener(t, func(c *Conn, _ []byte, _ func()) { got <- c }, 0)
		channel := tcpTransportDial(t, l)
		if err := channel.Send([]byte{1}); err != nil {
			t.Fatal(err)
		}
		server := <-got
		if err := l.Close(); err != nil {
			t.Fatalf("server close: %v", err)
		}
		if err := channel.Close(); err != nil {
			t.Fatalf("client close: %v", err)
		}
		<-server.Closed()
		if err := channel.Send([]byte{0xff}); !errors.Is(err, ErrConnClosed) {
			t.Fatalf("client send after close: %v", err)
		}
		if err := server.Send([]byte{0xff}); !errors.Is(err, ErrConnClosed) {
			t.Fatalf("server send after close: %v", err)
		}
	})
}
