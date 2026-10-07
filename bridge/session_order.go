// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/SukramJ/go-fabric/transport/message"
)

// A session's messages are handled in the order they arrive. The UDP
// listener used to give every datagram its own goroutine from the start,
// so two messages of one session could be handled in either order: a Read
// sent 6 ms behind an Invoke reported state from before the command
// (TC-IDM-1.2 step 7, about one run in four), and a StatusResponse acking
// report chunk N could be processed after chunk N+1 had already gone out
// with a stale piggybacked acknowledgement, which chip-tool drops ("Dropping
// message without piggyback ack when we are waiting for an ack") — the
// chunked read then stalled until the client's ReadClient timed out
// (TC-BINFO-2.1, the PICS generation's wildcard read). matter.js handles a
// session's messages in arrival order on its event loop
// (ExchangeManager #receiveMessage), chip on its device loop.
//
// The listener now keeps the datagrams of one unicast session in a queue
// (transport/udp ServeOrdered) and hands them over one at a time. A handler
// that sends and then waits for the peer's answer on the same session —
// a chunked report waiting for its StatusResponse, the subscription priming
// loop — releases its turn first (leaveSessionOrder), so the answer it
// waits for is not queued behind it. Unsecured messages (the PASE / CASE
// handshakes, session id 0) and group messages, which the bridge
// serialises itself, are not queued.

// sessionOrderKey names the stream of a datagram: its local session id, for
// a message of a unicast secure session.
func sessionOrderKey(buf []byte) (uint32, bool) {
	if len(buf) < 4 {
		return 0, false
	}
	// Session type 0 is unicast (message.SessionUnsecured names the value);
	// with session id 0 it is the unsecured session.
	if message.SessionType(buf[3]&secFlagSessionTypeBits) != message.SessionUnsecured {
		return 0, false
	}
	sid := binary.LittleEndian.Uint16(buf[1:3])
	if sid == 0 {
		return 0, false
	}
	return uint32(sid), true
}

type sessionTurnKey struct{}

// handleOrderedDatagram is the ordered listener's handler: the dispatch
// context carries the release of the session's turn.
func (b *Bridge) handleOrderedDatagram(buf []byte, src *net.UDPAddr, release func()) {
	ctx := context.WithValue(b.handlerContext(), sessionTurnKey{}, release)
	_ = b.dispatch(ctx, buf, src)
}

// leaveSessionOrder hands the session's turn to its next message: called
// before a handler waits for the peer's answer on the same session.
func leaveSessionOrder(ctx context.Context) {
	if release, ok := ctx.Value(sessionTurnKey{}).(func()); ok {
		release()
	}
}
