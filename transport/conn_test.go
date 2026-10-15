// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package transport_test

import (
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/transport"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestConnImplementations: both transports satisfy Conn, and only the
// TCP one is reliable (matter.js TcpChannel.ts:44; Session.ts:355-356).
func TestConnImplementations(t *testing.T) {
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	conns := map[string]transport.Conn{
		"udp": l.Conn(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}),
		"tcp": (*tcp.Conn)(nil),
	}
	if conns["udp"].Reliable() {
		t.Fatal("udp adapter reports reliable")
	}
	if !conns["tcp"].Reliable() {
		t.Fatal("tcp conn reports unreliable")
	}
}
