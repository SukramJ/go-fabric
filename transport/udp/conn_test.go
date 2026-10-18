// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"net"
	"testing"
	"time"
)

// TestConnAdapterSendsThroughListener: the UDP adapter of
// transport.Conn is the listener plus one peer address, unreliable.
func TestConnAdapterSendsThroughListener(t *testing.T) {
	a, b := newLoopbackPair(t)
	c := a.Conn(b.LocalAddr())
	if c.Reliable() {
		t.Fatal("UDP adapter reports reliable")
	}
	if c.Remote().String() != b.LocalAddr().String() || c.Addr() != b.LocalAddr() {
		t.Fatalf("remote = %v, want %v", c.Remote(), b.LocalAddr())
	}
	if err := c.Send([]byte{0x42}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	_ = b.conn.(*net.UDPConn).SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := b.conn.ReadFromUDP(buf)
	if err != nil || n != 1 || buf[0] != 0x42 {
		t.Fatalf("read n=%d err=%v", n, err)
	}
}
