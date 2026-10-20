// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package udpseam carries the one-way seam through which the module's test
// support (fabrictest's in-memory network) builds a transport/udp.Listener
// over a socket that is not a kernel one.
//
// Direction of the edge: package udp installs [NewListener] from its init;
// fabrictest reads it. The seam is module-internal, so no consumer sees a
// second constructor of the production listener: an external module gets
// the in-memory network only through fabrictest.
package udpseam

import "net"

// PacketConn is what a udp.Listener reads and writes: the subset of
// *net.UDPConn the listener uses. A listener over a PacketConn that is not
// a *net.UDPConn has no multicast — JoinGroup, LeaveGroup and SendMulticast
// report udp.ErrNoMulticastInterface — unless it carries the three itself,
// as fabrictest's in-memory socket does.
type PacketConn interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	LocalAddr() net.Addr
	Close() error
}

// NewListener returns a *udp.Listener (as any: this package must not import
// udp, which imports it to install the hook) serving conn. Installed by
// package udp's init.
var NewListener func(conn PacketConn) any
