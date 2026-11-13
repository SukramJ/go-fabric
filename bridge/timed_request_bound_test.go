// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestTimedRequestsOfOneSessionAreBounded pins the bound on the timed
// interactions a session may leave waiting for their Write or Invoke
// (review II, W2-conc-12): the one past [maxPendingTimedRequests] is
// answered Busy and registers no deadline. Before, every TimedRequest
// stored an entry, bounded only by the 16-bit exchange id space per
// session until the deadlines ran out. connectedhomeip bounds its timed
// handlers — a pool of CHIP_IM_MAX_NUM_TIMED_HANDLER = 8
// (src/lib/core/CHIPConfig.h:1010-1011 at 6170af84), node-wide — and
// answers Busy when it is exhausted
// (src/app/InteractionModelEngine.cpp:963-973). The bound here is per
// session, so one session cannot take another's.
func TestTimedRequestsOfOneSessionAreBounded(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 95
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	// send drives one TimedRequest of session on exchange through the
	// receive path.
	send := func(sess *channel.Session, session, exchange uint16, addr *net.UDPAddr) {
		t.Helper()
		datagram := encryptIMFromPeer(t, sess, session, message.ProtocolHeader{
			Opcode: im.OpcodeTimedRequest, ExchangeID: exchange, Initiator: true,
		}, encodeReliabilityTestTimedRequest(t, 30_000))
		if err := b.dispatch(context.Background(), datagram, addr); err != nil {
			t.Fatalf("dispatch of the TimedRequest on exchange %#x: %v", exchange, err)
		}
	}
	pending := func(session uint16) int {
		n := 0
		b.routing.timedDeadlines.Range(func(k, _ any) bool {
			if key, ok := k.(timedKey); ok && key.sessionID == session {
				n++
			}
			return true
		})
		return n
	}

	for i := range maxPendingTimedRequests {
		send(peerSess, sessionID, uint16(0x3000+i), peerAddr)
		_, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
		if !ok || decodeStatusResponsePayload(t, body) != im.StatusSuccess {
			t.Fatalf("TimedRequest %d was not answered Success", i+1)
		}
	}
	if n := pending(sessionID); n != maxPendingTimedRequests {
		t.Fatalf("pending timed requests = %d, want %d", n, maxPendingTimedRequests)
	}

	// One more on a new exchange: Busy, nothing stored.
	send(peerSess, sessionID, 0x3100, peerAddr)
	_, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok {
		t.Fatal("the TimedRequest past the bound was not answered")
	}
	if got := decodeStatusResponsePayload(t, body); got != im.StatusBusy {
		t.Errorf("TimedRequest past the bound answered %v, want Busy", got)
	}
	if n := pending(sessionID); n != maxPendingTimedRequests {
		t.Errorf("pending timed requests after the refused one = %d, want %d — it must store nothing", n, maxPendingTimedRequests)
	}

	// A TimedRequest again on an exchange that already waits replaces its
	// own deadline; another session's entries do not count.
	send(peerSess, sessionID, 0x3000, peerAddr)
	if _, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second); !ok || decodeStatusResponsePayload(t, body) != im.StatusSuccess {
		t.Error("a TimedRequest on an exchange that already waits was not answered Success")
	}
	if n := b.routing.pendingTimedRequests(sessionID+1, 0, time.Now()); n != 0 {
		t.Errorf("pending timed requests of another session = %d, want 0", n)
	}
}
