// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package mrp implements the Matter Message Reliability Protocol per
// Matter Core Specification §4.12.
//
// MRP rides on top of the unreliable UDP transport and adds:
//
//   - Per-session monotonic 32-bit message counters
//     (initialized to a random value at session creation).
//   - Sliding-window duplicate detection across the most recent 32
//     received counters.
//   - The retransmission schedule for messages flagged "Reliable"
//     (Exchange Flag R = 1): [BackoffDuration] and the
//     [MaxRetransmissions] cap. The tracker that re-sends until an ACK
//     arrives lives in the bridge (bridge/outbound_reliable.go); the
//     [Retransmitter] here is an unwired stand-alone variant.
//   - Standalone-ACK synthesis ([AckTracker]) when no payload is
//     queued to piggyback the acknowledgement on. The dispatcher
//     drains [AckTracker.Due] and emits a Secure-Channel
//     StandaloneAck per Matter §4.12.7 (opcode [StandaloneAckOpcode]
//     under [SecureChannelProtocolID]).
//
// The primitives in this package are stateful but I/O-free. The bridge
// ([..]/bridge) drives them: its receive path feeds [Window] and
// [AckTracker], its outbound tracker paces re-sends from
// [BackoffDuration]. The UDP transport ([..]/transport/udp) only moves
// datagrams, so MRP itself is unit-tested without sockets.
package mrp
