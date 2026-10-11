// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package tcp implements the Matter TCP transport: the length-framed
// stream Matter uses for large payloads (Matter Core Specification
// §4.3). A port of matter.js
// packages/protocol/src/transport/tcp/TcpChannel.ts (the per-connection
// framing) and TcpTransport.ts (the listener, the dialer and the
// connection registry).
//
// Framing: every Matter message is preceded by its length as a 4-byte
// little-endian uint32 (TcpChannel.ts:37-41, FRAMING_HEADER_SIZE = 4 at
// :28). The configured maximum payload size counts the whole frame, so
// a message may carry at most MaxPayloadSize-4 bytes (:114-116); a
// larger send is refused (:125-126) and a larger announced length, or a
// zero one, closes the connection (:260-271). Received bytes not yet
// forming a message are bounded at twice the maximum payload size, past
// which the connection is closed (MAX_RECEIVE_BUFFER_FACTOR = 2, :32,
// :212-218).
//
// Listener (TcpTransport.ts:78-110): at most three connections per peer
// IP address (MAX_CONNECTIONS_PER_PEER_IP, :27-28); an inbound
// connection that has not delivered a complete message within ten
// seconds is closed (NEW_CONNECTION_IDLE_TIMEOUT, :30-31, :89-104).
//
// A connection is reliable ([Conn.Reliable] is true): the layers above
// do not run MRP over it (matter.js Session.ts:355-356). Each
// connection's messages are handed to the handler in arrival order,
// one at a time, each starting once its predecessor returned or
// released its turn — the same contract as transport/udp ServeOrdered,
// for the same reason: a handler that waits for the peer's answer on
// the same connection must let that answer through.
//
// IPv6 first, like transport/udp: the default listener is the dual-stack
// `[::]:5540`.
package tcp
