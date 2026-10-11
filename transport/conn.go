// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package transport names what the layers above the wire need from a
// Matter transport, independent of which one carried a message: the
// connection a request arrived on, so the reply goes back on it.
//
// The concrete transports live in the sub-packages: transport/udp
// (datagrams, MRP on top) and transport/tcp (length-framed stream,
// reliable). Mirrors matter.js's split between the channel abstraction
// and its UDP / TCP implementations, of which the TCP one declares
// itself reliable
// (packages/protocol/src/transport/tcp/TcpChannel.ts:44
// `readonly isReliable = true as const`).
package transport

import "net"

// Conn is the connection a Matter message arrived on and its reply
// leaves on.
//
// Reliable reports whether the carrier itself guarantees delivery. On
// a reliable connection Matter's MRP is not used: no retransmission,
// no acknowledgement requested, no standalone or piggybacked
// acknowledgement sent. Mirrors matter.js
// packages/protocol/src/session/Session.ts:355-356
// (`usesMrp = supportsMRP && !channel?.isReliable`).
type Conn interface {
	// Send ships one complete Matter message (message header and
	// payload). The transport adds whatever framing it needs.
	Send(msg []byte) error
	// Remote is the peer's address.
	Remote() net.Addr
	// Reliable reports whether the carrier guarantees delivery.
	Reliable() bool
}
