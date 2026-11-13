// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// White-box tests for the end of a chunk wait — the handler that has sent
// a chunk of a priming report or of a chunked read and waits for the
// peer's StatusResponse — and for the number of such waits one session may
// hold (review II, R2-P1-req2-01).

// chunkWaitPeer is one session's peer side: its socket, its half of the
// session and its address.
type chunkWaitPeer struct {
	id   uint16
	conn *net.UDPConn
	sess *channel.Session
	addr *net.UDPAddr
}

// pasePeersLookup resolves every session it holds and marks each one
// PASE, so a wildcard read is served in full without an ACL.
type pasePeersLookup map[uint16]*channel.Session

func (l pasePeersLookup) Lookup(id uint16) (*channel.Session, bool) {
	s, ok := l[id]
	return s, ok
}

func (l pasePeersLookup) IsPASE(id uint16) (pase, ok bool) {
	_, ok = l[id]
	return ok, ok
}

// chunkWaitPeers wires one PASE session per id onto b, each with its own
// peer socket.
func chunkWaitPeers(t *testing.T, b *Bridge, ids ...uint16) []chunkWaitPeer {
	t.Helper()
	lookup := pasePeersLookup{}
	peers := make([]chunkWaitPeer, 0, len(ids))
	for _, id := range ids {
		conn, peerSess, addr := chunkTestPeer(t, b, id)
		b.mu.RLock()
		bridgeSess, ok := b.sessions.Lookup(id)
		b.mu.RUnlock()
		if !ok {
			t.Fatalf("session %d not attached", id)
		}
		lookup[id] = bridgeSess
		peers = append(peers, chunkWaitPeer{id: id, conn: conn, sess: peerSess, addr: addr})
	}
	b.AttachSessionLookup(lookup)
	return peers
}

// sendToBridge writes a datagram from the peer's socket to b's listener:
// the real receive path, ordered dispatch and handler context included.
func sendToBridge(t *testing.T, b *Bridge, p chunkWaitPeer, datagram []byte) {
	t.Helper()
	to, err := net.ResolveUDPAddr("udp", b.LocalAddr())
	if err != nil {
		t.Fatalf("the bridge's address %q: %v", b.LocalAddr(), err)
	}
	to.IP = net.IPv4(127, 0, 0, 1)
	if _, err := p.conn.WriteToUDP(datagram, to); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}
}

// wildcardRead seals a wildcard ReadRequest from p on exchange.
func wildcardRead(t *testing.T, p chunkWaitPeer, exchange uint16) []byte {
	t.Helper()
	return encryptIMFromPeer(t, p.sess, p.id, message.ProtocolHeader{
		Opcode: im.OpcodeReadRequest, ExchangeID: exchange, Initiator: true,
	}, buildWildcardReadRequestPayload(t))
}

// TestChunkWaitsOfOneSessionAreBounded: a session that starts chunked
// reads and never answers their first chunks parks at most
// [maxChunkWaitsPerSession] handlers; the read beyond is answered Busy at
// once, and another session's read is still dispatched and answered. Each
// parked handler held a slot of the listener's bridge-wide dispatch
// budget until its wait ended, so one peer could fill the budget.
func TestChunkWaitsOfOneSessionAreBounded(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute // only the bound can answer in time
	peers := chunkWaitPeers(t, b, 61, 62)
	a, other := peers[0], peers[1]

	const extra = 2
	for i := range maxChunkWaitsPerSession + extra {
		sendToBridge(t, b, a, wildcardRead(t, a, uint16(0x3100+i)))
	}
	var parked, busy int
	for range maxChunkWaitsPerSession + extra {
		proto, body, ok := readOneIMDatagram(t, a.conn, a.sess, 3*time.Second)
		if !ok {
			break
		}
		switch proto.Opcode {
		case im.OpcodeReportData:
			parked++
		case im.OpcodeStatusResponse:
			if got := decodeStatusResponsePayload(t, body); got != im.StatusBusy {
				t.Errorf("the read beyond the bound was answered %v, want Busy", got)
			}
			busy++
		default:
			t.Errorf("unexpected opcode %#x", proto.Opcode)
		}
	}
	if parked != maxChunkWaitsPerSession || busy != extra {
		t.Fatalf("session %d: %d reads parked on their first chunk and %d answered Busy; want %d and %d", a.id, parked, busy, maxChunkWaitsPerSession, extra)
	}

	sendToBridge(t, b, other, wildcardRead(t, other, 0x3200))
	proto, _, ok := readOneIMDatagram(t, other.conn, other.sess, 3*time.Second)
	if !ok || proto.Opcode != im.OpcodeReportData {
		t.Fatalf("session %d's read was not answered with a ReportData (got=%v opcode %#x)", other.id, ok, proto.Opcode)
	}
}

// TestStopEndsPendingChunkWaits: Stop cancels the handler context, and a
// handler parked on a chunk's StatusResponse returns with it — Stop's wait
// for the handlers in flight no longer runs out the chunk bound.
func TestStopEndsPendingChunkWaits(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	a := chunkWaitPeers(t, b, 63)[0]
	for i := range 2 {
		sendToBridge(t, b, a, wildcardRead(t, a, uint16(0x3300+i)))
	}
	for range 2 {
		if _, _, ok := readOneIMDatagram(t, a.conn, a.sess, 3*time.Second); !ok {
			t.Fatal("a read's first chunk did not reach the peer")
		}
	}

	stopCtx, stopCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer stopCancel()
	stopped := make(chan error, 1)
	go func() { stopped <- b.Stop(stopCtx) }()
	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
	case <-wait.Done():
		t.Fatal("Stop still waits after 5 s on handlers parked on unanswered chunks")
	}
}

// TestSessionCloseEndsItsChunkWaits: the close of a session ends the
// chunk waits of its handlers at once.
func TestSessionCloseEndsItsChunkWaits(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	a := chunkWaitPeers(t, b, 64)[0]

	done := make(chan error, 1)
	go func() { done <- b.dispatch(context.Background(), wildcardRead(t, a, 0x3400), a.addr) }()
	if _, _, ok := readOneIMDatagram(t, a.conn, a.sess, 3*time.Second); !ok {
		t.Fatal("the read's first chunk did not reach the peer")
	}
	b.SessionClosed(a.id)

	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the read returned nil after its session closed mid-wait; want the abort")
		}
	case <-wait.Done():
		t.Fatal("the read still waits for its chunk's answer 5 s after its session closed")
	}
}

// TestSubscriptionCloseEndsItsPrimingWait: a SubscribeRequest with
// KeepSubscriptions=false closes the session's subscription still
// priming, and the handler streaming that priming report stops waiting.
func TestSubscriptionCloseEndsItsPrimingWait(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	a := chunkWaitPeers(t, b, 65)[0]
	mgr := subscription.NewManager(subscription.Config{}, b.SubscriptionReporter(), nil)
	b.AttachSubscriptionManager(mgr)

	subscribe := func(exchange uint16) []byte {
		enc := tlv.NewEncoder()
		im.SubscribeRequest{MaxIntervalCeiling: 60, AttributeRequests: []im.ConcreteAttributePath{{}}}.MarshalTLV(enc)
		payload, err := enc.Bytes()
		if err != nil {
			t.Fatalf("SubscribeRequest.MarshalTLV: %v", err)
		}
		return encryptIMFromPeer(t, a.sess, a.id, message.ProtocolHeader{
			Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchange, Initiator: true,
		}, payload)
	}
	first := make(chan error, 1)
	go func() { first <- b.dispatch(context.Background(), subscribe(0x3500), a.addr) }()
	if _, _, ok := readOneIMDatagram(t, a.conn, a.sess, 3*time.Second); !ok {
		t.Fatal("the first subscription's priming chunk did not reach the peer")
	}
	go func() { _ = b.dispatch(context.Background(), subscribe(0x3501), a.addr) }()

	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case err := <-first:
		if err == nil {
			t.Error("the first priming returned nil after its subscription was closed")
		}
	case <-wait.Done():
		t.Fatal("the first priming still waits 5 s after the second subscribe closed its subscription")
	}
	b.SessionClosed(a.id) // ends the second priming's wait
}
