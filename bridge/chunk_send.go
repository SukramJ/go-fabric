// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// chunkLeg is how one interaction's chunks go out and how the wait for
// the peer's StatusResponse to each is bounded: what the read reply, the
// priming report of a SubscribeRequest, an ongoing report, a
// re-established subscription's priming report and a chunked
// InvokeResponse differ in, each stated once (review II, R2-P5-write-07).
// [Bridge.sendChunk] is the one arm → send → wait they share; matter.js
// sends every chunk through sendDataReportMessage, which waits for the
// peer's Success before the next is sent
// (packages/protocol/src/interaction/InteractionMessenger.ts:799-844 at
// db021da0).
type chunkLeg struct {
	// op names the leg in the log ("read", "subscribe", "invoke", …).
	op  string
	src net.Addr
	// sessionID, exchangeID and role key the StatusResponse wait: role
	// is this node's initiator flag on the exchange.
	sessionID, exchangeID uint16
	role                  bool
	// send puts chunk i's encoded body on the wire with the
	// acknowledgement the bridge owes on the exchange piggybacked, and
	// returns the MRP counter it was tracked under (0: untracked).
	send func(i int, body []byte) (counter uint32, err error)
	// timeout bounds the wait; zero is the exchange's chunk bound
	// ([Bridge.chunkStatusResponseTimeout]).
	timeout time.Duration
	// done and abort end the wait with [errChunkWaitAborted] when either
	// closes (nil: never): done is the handler's context, abort the
	// subscription's or the engine's stop.
	done, abort <-chan struct{}
	// beforeWait runs after a chunk that is waited for went out and
	// before the wait: a request's handler hands its session's turn on,
	// as the answer arrives on that session.
	beforeWait func()
}

// chunkWait is what [Bridge.sendChunk] does about the answer to a chunk.
type chunkWait uint8

const (
	// chunkNoWait sends the chunk and arms nothing: a read's last chunk,
	// which suppresses the answer, and an ongoing report's last chunk,
	// whose answer the report's own bookkeeping consumes.
	chunkNoWait chunkWait = iota
	// chunkWaitHere waits for the answer before sendChunk returns.
	chunkWaitHere
	// chunkWaitCaller arms the wait before the send and hands it to the
	// caller, who awaits it.
	chunkWaitCaller
)

// chunkSent is what [Bridge.sendChunk] reports of a chunk.
type chunkSent struct {
	// sent is true once the chunk went out: an error with it is the
	// wait's, without it the send's.
	sent bool
	// counter is the MRP counter the chunk was tracked under (0:
	// untracked).
	counter uint32
	// armed is the wait handed to the caller (chunkWaitCaller).
	armed <-chan statusAnswer
}

// sendChunk sends chunk i, encoded as body, on leg's exchange. A wait is
// armed before the send — the answer can arrive before the send returns —
// and disarmed when the send fails. With chunkWaitHere it waits for the
// answer: a status other than Success, a missing answer, done, abort or
// the session's close end it with an error ([Bridge.awaitChunkStatusResponseWithin]).
// With chunkWaitCaller it returns the armed wait.
func (b *Bridge) sendChunk(leg chunkLeg, i int, body []byte, chunk im.ReportData, wait chunkWait) (chunkSent, error) {
	var waitCh <-chan statusAnswer
	if wait != chunkNoWait {
		waitCh = b.armStatusResponseWait(leg.sessionID, leg.exchangeID, leg.role)
	}
	counter, err := leg.send(i, body)
	if err != nil {
		if waitCh != nil {
			b.disarmStatusResponseWait(leg.sessionID, leg.exchangeID, leg.role)
		}
		return chunkSent{}, err
	}
	out := chunkSent{sent: true, counter: counter}
	if wait != chunkWaitHere {
		out.armed = waitCh
		return out, nil
	}
	if leg.beforeWait != nil {
		leg.beforeWait()
	}
	timeout := leg.timeout
	if timeout <= 0 {
		timeout = b.chunkStatusResponseTimeout(leg.src, leg.sessionID)
	}
	return out, b.awaitChunkStatusResponseWithin(timeout, leg.done, leg.abort, waitCh, leg.op, leg.src, leg.sessionID, leg.exchangeID, leg.role, i, chunk)
}
