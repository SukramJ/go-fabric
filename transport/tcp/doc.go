// SPDX-License-Identifier: Apache-2.0
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
// :28). The configured maximum payload size is MAX_TCP_MESSAGE_SIZE,
// the length prefix not counted, so a message may carry MaxPayloadSize
// bytes (TcpChannel.ts:37-41 at db021da0); a larger send is refused
// (:116-120) and a larger announced length, or a zero one, closes the
// connection (:241-256). The received bytes have no cap of their own: the
// header check bounds an incomplete message (:204).
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
