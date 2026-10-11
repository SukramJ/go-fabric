// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type received struct {
	conn *Conn
	msg  []byte
}

// startListener binds a loopback listener on an ephemeral port and
// serves it with handler; the returned channel carries every closed
// connection.
func startListener(t *testing.T, handler Handler, idle time.Duration) (*Listener, <-chan *Conn) {
	t.Helper()
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0", MaxPayloadSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	if idle > 0 {
		l.idleTimeout = idle
	}
	closed := make(chan *Conn, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, handler, func(c *Conn) { closed <- c }) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return l, closed
}

func dialRaw(t *testing.T, l *Listener) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func readFrame(t *testing.T, c net.Conn) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("read header: %v", err)
	}
	body := make([]byte, binary.LittleEndian.Uint32(hdr))
	if _, err := io.ReadFull(c, body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func expectEOF(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection still open, want it closed by the listener")
	}
}

func TestListenerEchoRoundTrip(t *testing.T) {
	got := make(chan received, 4)
	l, _ := startListener(t, func(c *Conn, msg []byte, _ func()) {
		got <- received{c, msg}
		if err := c.Send(append([]byte("re:"), msg...)); err != nil {
			t.Errorf("send: %v", err)
		}
	}, 0)
	c := dialRaw(t, l)
	if _, err := c.Write(frameOf([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	r := <-got
	if string(r.msg) != "hello" || !r.conn.Reliable() || !r.conn.Incoming() || r.conn.Remote() == nil {
		t.Fatalf("received %q reliable=%v incoming=%v", r.msg, r.conn.Reliable(), r.conn.Incoming())
	}
	if r.conn.MaxMessageSize() != 60 {
		t.Fatalf("MaxMessageSize = %d, want 60", r.conn.MaxMessageSize())
	}
	if reply := readFrame(t, c); string(reply) != "re:hello" {
		t.Fatalf("reply %q", reply)
	}
	if err := r.conn.Send(make([]byte, 61)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversized send: %v", err)
	}
	if r.conn.String() == "" {
		t.Fatal("empty connection name")
	}
}

func TestListenerDeliversInOrderAndReleaseLetsNextThrough(t *testing.T) {
	var mu sync.Mutex
	var order []string
	gate := make(chan struct{})
	secondSeen := make(chan struct{})
	l, _ := startListener(t, func(_ *Conn, msg []byte, release func()) {
		mu.Lock()
		order = append(order, string(msg))
		mu.Unlock()
		switch string(msg) {
		case "a":
			// Waits for something only the next message brings: must
			// release its turn first.
			release()
			<-gate
		case "b":
			close(secondSeen)
		}
	}, 0)
	c := dialRaw(t, l)
	var stream []byte
	for _, m := range []string{"a", "b"} {
		stream = append(stream, frameOf([]byte(m))...)
	}
	if _, err := c.Write(stream); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("second message blocked behind a handler that released its turn")
	}
	close(gate)
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
}

func TestListenerClosesOnMalformedLength(t *testing.T) {
	l, closed := startListener(t, func(*Conn, []byte, func()) {}, 0)
	c := dialRaw(t, l)
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	expectEOF(t, c)
	if cc := <-closed; !errors.Is(cc.Err(), ErrZeroLengthFrame) {
		t.Fatalf("close reason = %v", cc.Err())
	}

	c2 := dialRaw(t, l)
	if _, err := c2.Write([]byte{61, 0, 0, 0}); err != nil { // 61 > 64-4
		t.Fatal(err)
	}
	expectEOF(t, c2)
	if cc := <-closed; !errors.Is(cc.Err(), ErrFrameTooLarge) {
		t.Fatalf("close reason = %v", cc.Err())
	}
}

func TestListenerIdleTimeoutClosesSilentConnection(t *testing.T) {
	l, closed := startListener(t, func(*Conn, []byte, func()) {}, 50*time.Millisecond)
	c := dialRaw(t, l)
	expectEOF(t, c)
	if cc := <-closed; !errors.Is(cc.Err(), errIdle) {
		t.Fatalf("close reason = %v", cc.Err())
	}
}

func TestListenerIdleTimeoutStopsAtFirstMessage(t *testing.T) {
	got := make(chan struct{}, 1)
	l, _ := startListener(t, func(*Conn, []byte, func()) { got <- struct{}{} }, 50*time.Millisecond)
	c := dialRaw(t, l)
	if _, err := c.Write(frameOf([]byte{1})); err != nil {
		t.Fatal(err)
	}
	<-got
	time.Sleep(150 * time.Millisecond)
	if l.Conns() != 1 {
		t.Fatalf("connection closed after its first message: conns=%d", l.Conns())
	}
}

func TestListenerPerPeerIPCap(t *testing.T) {
	l, closed := startListener(t, func(*Conn, []byte, func()) {}, 0)
	for range MaxConnectionsPerPeerIP {
		dialRaw(t, l)
	}
	deadline := time.Now().Add(5 * time.Second)
	for l.Conns() < MaxConnectionsPerPeerIP && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	fourth := dialRaw(t, l)
	expectEOF(t, fourth)
	if l.Conns() != MaxConnectionsPerPeerIP {
		t.Fatalf("conns = %d, want %d", l.Conns(), MaxConnectionsPerPeerIP)
	}
	select {
	case c := <-closed:
		t.Fatalf("a rejected connection was reported as a registered one closing: %v", c)
	default:
	}
}

func TestListenerCloseClosesConnectionsAndNotifies(t *testing.T) {
	got := make(chan *Conn, 1)
	l, closed := startListener(t, func(c *Conn, _ []byte, _ func()) { got <- c }, 0)
	c := dialRaw(t, l)
	if _, err := c.Write(frameOf([]byte{1})); err != nil {
		t.Fatal(err)
	}
	conn := <-got
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if cc := <-closed; cc != conn {
		t.Fatal("onClose reported another connection")
	}
	<-conn.Closed()
	if err := conn.Send([]byte{1}); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("send after close: %v", err)
	}
	if err := l.Serve(context.Background(), func(*Conn, []byte, func()) {}, nil); !errors.Is(err, ErrListenerClosed) {
		t.Fatalf("Serve on closed listener: %v", err)
	}
}

func TestListenerPeerCloseNotifies(t *testing.T) {
	got := make(chan *Conn, 1)
	l, closed := startListener(t, func(c *Conn, _ []byte, _ func()) { got <- c }, 0)
	c := dialRaw(t, l)
	if _, err := c.Write(frameOf([]byte{1})); err != nil {
		t.Fatal(err)
	}
	conn := <-got
	_ = c.Close()
	if cc := <-closed; cc != conn {
		t.Fatal("onClose reported another connection")
	}
}

func TestListenerRecoversHandlerPanic(t *testing.T) {
	got := make(chan struct{}, 2)
	l, _ := startListener(t, func(_ *Conn, msg []byte, _ func()) {
		got <- struct{}{}
		if msg[0] == 1 {
			panic("boom")
		}
	}, 0)
	c := dialRaw(t, l)
	if _, err := c.Write(append(frameOf([]byte{1}), frameOf([]byte{2})...)); err != nil {
		t.Fatal(err)
	}
	<-got
	<-got
	if l.RecoveredPanics() != 1 {
		t.Fatalf("recovered = %d", l.RecoveredPanics())
	}
}

func TestListenNilHandlerAndBadAddress(t *testing.T) {
	if _, err := Listen(Config{LocalAddr: "not-an-address"}); err == nil {
		t.Fatal("Listen accepted a malformed address")
	}
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Serve(context.Background(), nil, nil); err == nil {
		t.Fatal("Serve accepted a nil handler")
	}
	if l.maxPayloadSize != DefaultMaxPayloadSize {
		t.Fatalf("default max payload = %d", l.maxPayloadSize)
	}
}

func TestBindAddrDefaultsIPv6First(t *testing.T) {
	if n, a := bindAddr(Config{}); n != "tcp" || a != "[::]:5540" {
		t.Fatalf("default bind = %s %s", n, a)
	}
	if n, a := bindAddr(Config{PreferIPv4: true}); n != "tcp4" || a != "0.0.0.0:5540" {
		t.Fatalf("IPv4 bind = %s %s", n, a)
	}
}

func TestDialerRoundTrip(t *testing.T) {
	l, _ := startListener(t, func(c *Conn, msg []byte, _ func()) {
		_ = c.Send(bytes.ToUpper(msg))
	}, 0)
	replies := make(chan []byte, 1)
	var d Dialer
	conn, err := d.Dial(context.Background(), l.Addr().String(), func(_ *Conn, msg []byte, _ func()) { replies <- msg })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if conn.Incoming() || !conn.Reliable() {
		t.Fatalf("incoming=%v reliable=%v", conn.Incoming(), conn.Reliable())
	}
	if err := conn.Send([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-replies:
		if string(r) != "PING" {
			t.Fatalf("reply %q", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	if _, err := d.Dial(context.Background(), l.Addr().String(), nil); err == nil {
		t.Fatal("Dial accepted a nil handler")
	}
}
