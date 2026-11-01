// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"
)

// scriptedConn hands out one read result per call from its script, then
// blocks until closed.
type scriptedConn struct {
	mu     sync.Mutex
	script []scriptedRead
	closed chan struct{}
	once   sync.Once
}

type scriptedRead struct {
	data []byte
	err  error
}

func newScriptedConn(reads ...scriptedRead) *scriptedConn {
	return &scriptedConn{script: reads, closed: make(chan struct{})}
}

func (c *scriptedConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	c.mu.Lock()
	if len(c.script) > 0 {
		r := c.script[0]
		c.script = c.script[1:]
		c.mu.Unlock()
		if r.err != nil {
			return 0, nil, r.err
		}
		return copy(b, r.data), &net.UDPAddr{IP: net.IPv6loopback, Port: 5541}, nil
	}
	c.mu.Unlock()
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (c *scriptedConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }

func (c *scriptedConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv6loopback, Port: MatterPort}
}

func (c *scriptedConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// A receive error the socket survives is logged and the loop keeps
// reading, as chip's UDP transport keeps its endpoint listening
// (src/transport/raw/UDP.cpp:130-133 at 6170af84, OnUdpError): the
// datagram after it is still handled.
func TestServeContinuesAfterATransientReadError(t *testing.T) {
	t.Parallel()
	conn := newScriptedConn(
		scriptedRead{err: syscall.ECONNREFUSED},
		scriptedRead{data: []byte{0x01, 0x02}},
	)
	l := newListener(conn)
	got := make(chan []byte, 1)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- l.Serve(ctx, func(buf []byte, _ *net.UDPAddr) { got <- append([]byte(nil), buf...) })
	}()
	select {
	case b := <-got:
		if len(b) != 2 {
			t.Fatalf("handled %x, want the datagram after the error", b)
		}
	case err := <-served:
		t.Fatalf("Serve returned %v after a transient read error", err)
	case <-time.After(5 * time.Second):
		t.Fatal("datagram after the read error never handled")
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("Serve after cancel = %v, want nil", err)
	}
}

// A socket that is gone ends the loop with the error, so the caller sees
// a node that no longer receives.
func TestServeExitsWhenTheSocketIsGone(t *testing.T) {
	t.Parallel()
	l := newListener(newScriptedConn(scriptedRead{err: syscall.EBADF}))
	err := l.Serve(context.Background(), func([]byte, *net.UDPAddr) {})
	if !errors.Is(err, syscall.EBADF) {
		t.Fatalf("Serve = %v, want EBADF", err)
	}
	_ = l.Close()
}

// A socket that fails every read gives up after maxConsecutiveReadErrors.
func TestServeGivesUpOnAPersistentlyFailingSocket(t *testing.T) {
	t.Parallel()
	reads := make([]scriptedRead, maxConsecutiveReadErrors)
	for i := range reads {
		reads[i] = scriptedRead{err: syscall.ENOBUFS}
	}
	l := newListener(newScriptedConn(reads...))
	err := l.Serve(context.Background(), func([]byte, *net.UDPAddr) {})
	if !errors.Is(err, syscall.ENOBUFS) {
		t.Fatalf("Serve = %v, want ENOBUFS after %d failed reads", err, maxConsecutiveReadErrors)
	}
	_ = l.Close()
}
