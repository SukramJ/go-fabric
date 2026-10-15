// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package udp

import (
	"net"

	"github.com/SukramJ/go-fabric/transport"
)

// Conn is the UDP adapter of [transport.Conn]: the listener a datagram
// arrived on plus the peer address it came from. UDP is not reliable,
// so Matter's MRP runs on top of it.
type Conn struct {
	l   *Listener
	dst *net.UDPAddr
}

var _ transport.Conn = (*Conn)(nil)

// Conn returns the connection to dst over this listener's socket.
func (l *Listener) Conn(dst *net.UDPAddr) *Conn {
	return &Conn{l: l, dst: dst}
}

// Send ships msg as one datagram to the peer ([Listener.Send]).
func (c *Conn) Send(msg []byte) error {
	return c.l.Send(c.dst, msg)
}

// Remote is the peer's address.
func (c *Conn) Remote() net.Addr { return c.dst }

// Addr is the peer's address in its UDP type.
func (c *Conn) Addr() *net.UDPAddr { return c.dst }

// Reliable is false: a datagram may be lost, and MRP covers that.
func (c *Conn) Reliable() bool { return false }
