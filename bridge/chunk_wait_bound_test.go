// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
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

// TestSubscriptionCloseEndsItsPrimingWait: the close of a subscription
// still priming ends the wait of the handler streaming its priming report.
func TestSubscriptionCloseEndsItsPrimingWait(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = time.Minute
	a := chunkWaitPeers(t, b, 65)[0]
	mgr := subscription.NewManager(subscription.Config{}, b.SubscriptionReporter(), nil)
	b.AttachSubscriptionManager(mgr)

	enc := tlv.NewEncoder()
	im.SubscribeRequest{MaxIntervalCeiling: 60, AttributeRequests: []im.ConcreteAttributePath{{}}}.MarshalTLV(enc)
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatalf("SubscribeRequest.MarshalTLV: %v", err)
	}
	subscribe := encryptIMFromPeer(t, a.sess, a.id, message.ProtocolHeader{
		Opcode: im.OpcodeSubscribeRequest, ExchangeID: 0x3500, Initiator: true,
	}, payload)
	priming := make(chan error, 1)
	go func() { priming <- b.dispatch(context.Background(), subscribe, a.addr) }()
	if _, _, ok := readOneIMDatagram(t, a.conn, a.sess, 3*time.Second); !ok {
		t.Fatal("the subscription's priming chunk did not reach the peer")
	}
	open := mgr.Snapshot()
	if len(open) != 1 {
		t.Fatalf("%d subscriptions open, want the priming one", len(open))
	}
	if err := mgr.Close(open[0].ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case err := <-priming:
		if err == nil {
			t.Error("the priming returned nil after its subscription was closed")
		}
	case <-wait.Done():
		t.Fatal("the priming still waits 5 s after its subscription was closed")
	}
}

// TestReestablishPrimingWaitEnds (review II, R2-P5-write-07): the priming
// report of a re-established subscription waits for each chunk's answer
// through the same chunk sender as the other legs, so its wait ends with
// the bridge's handler context — which Stop cancels; an abort, no verdict
// on the peer — and with its session's close, which says the peer is
// gone; neither runs out the chunk bound. The wait lives as long as the
// bridge, not as long as the call that started the re-establishment.
func TestReestablishPrimingWaitEnds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		end  func(b *Bridge, id uint16)
		want error
	}{
		{"the bridge's handler context ends", func(b *Bridge, _ uint16) {
			b.mu.RLock()
			cancel := b.serveCancel
			b.mu.RUnlock()
			cancel()
		}, errChunkWaitAborted},
		{"the session closes", func(b *Bridge, id uint16) { b.SessionClosed(id) }, errPeerGone},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			b.chunkStatusResponseTimeoutOverride = time.Minute
			p := chunkWaitPeers(t, b, 65)[0]
			target := subTarget{src: p.addr, sessionID: p.id, exchangeID: 0x3500}
			reports := make([]im.AttributeReport, 200)
			for i := range reports {
				reports[i] = im.AttributeReport{
					Path:  im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: uint32(i), HasEndpoint: true, HasCluster: true, HasAttribute: true},
					Value: im.AttributeValue{Value: uint32(i)},
				}
			}
			chunker := newReportChunker(im.ReportData{HasSubscription: true, SubscriptionID: 9}, reportChunkPayloadBudget, reportChunkHardCap, sliceAttributeReports(reports), nil)
			done := make(chan error, 1)
			go func() { done <- b.sendPrimingReport(target, chunker) }()
			if _, _, ok := readOneIMDatagram(t, p.conn, p.sess, 3*time.Second); !ok {
				t.Fatal("the priming report's first chunk did not reach the peer")
			}
			c.end(b, p.id)
			wait, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			select {
			case err := <-done:
				if !errors.Is(err, c.want) {
					t.Errorf("the priming report ended with %v, want %v", err, c.want)
				}
			case <-wait.Done():
				t.Fatal("the priming report still waits for its chunk's answer 5 s later")
			}
		})
	}
}

// TestChunkWaitsBoundTheBatchInvoke: a batch invoke takes one of the
// session's chunk wait claims before any command runs, so a session that
// holds all [maxChunkWaitsPerSession] gets its batch answered Busy and the
// claims stay as they were (R2-P6-bound-0803); a single-command invoke
// never waits for a chunk's answer, takes no claim and is served at the
// bound (R2-P6-bound-0809).
func TestChunkWaitsBoundTheBatchInvoke(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	for range maxChunkWaitsPerSession {
		if !b.routing.chunkWaits.claim(paseTestSessionID, 0, 0) {
			t.Fatal("a claim below the bound was refused")
		}
	}
	claims := func() int {
		b.routing.chunkWaits.mu.Lock()
		defer b.routing.chunkWaits.mu.Unlock()
		return b.routing.chunkWaits.n[paseTestSessionID]
	}
	on := im.ConcreteCommandPath{Endpoint: unreliabilityTestEndpoint, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}
	off := im.ConcreteCommandPath{Endpoint: unreliabilityTestEndpoint, Cluster: 0x0006, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true}
	invoke := func(exchange uint16, payload []byte) uint8 {
		t.Helper()
		proto := message.ProtocolHeader{
			ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeInvokeRequest,
			ExchangeID: exchange, Initiator: true,
		}.Marshal()
		got, ok := dispatchInvokeCapture(t, b, secureTestDatagram(t, b, proto, payload), 2*time.Second)
		if !ok {
			t.Fatalf("the invoke on exchange %#x was not answered", exchange)
		}
		_, hdrLen, err := message.UnmarshalHeader(got)
		if err != nil {
			t.Fatal(err)
		}
		rproto, _, err := message.UnmarshalProtocolHeader(got[hdrLen:])
		if err != nil {
			t.Fatal(err)
		}
		if rproto.Opcode == im.OpcodeStatusResponse {
			if s := decodeWriteChunkedStatusResponse(t, got); s != im.StatusBusy {
				t.Fatalf("the invoke on exchange %#x answered %v", exchange, s)
			}
		}
		return rproto.Opcode
	}

	if op := invoke(0x3500, encodeInvokeRequestWithRefs(t, on, off)); op != im.OpcodeStatusResponse {
		t.Errorf("a batch invoke at the bound answered with opcode %#x, want StatusResponse Busy", op)
	}
	if n := claims(); n != maxChunkWaitsPerSession {
		t.Errorf("claims after the refused batch = %d, want %d", n, maxChunkWaitsPerSession)
	}
	if op := invoke(0x3501, encodeInvokeRequestSuppress(t, false, on)); op != im.OpcodeInvokeResponse {
		t.Errorf("a single-command invoke at the bound answered with opcode %#x, want InvokeResponse", op)
	}
	if n := claims(); n != maxChunkWaitsPerSession {
		t.Errorf("claims after the single invoke = %d, want %d", n, maxChunkWaitsPerSession)
	}
	b.routing.chunkWaits.release(paseTestSessionID, 0)
	if op := invoke(0x3502, encodeInvokeRequestWithRefs(t, on, off)); op != im.OpcodeInvokeResponse {
		t.Errorf("a batch invoke below the bound answered with opcode %#x, want InvokeResponse", op)
	}
	if n := claims(); n != maxChunkWaitsPerSession-1 {
		t.Errorf("claims after the served batch = %d, want %d (its claim given back)", n, maxChunkWaitsPerSession-1)
	}
}

// TestEndSessionChunkWaitsEndsOnlyItsOwn: the end of one session's chunk
// waits leaves another session's wait armed (R2-P5-tmut-20, bridge's own
// test of a mutant only fabrictest caught).
func TestEndSessionChunkWaitsEndsOnlyItsOwn(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	mine := b.armStatusResponseWait(1, 0x10, false)
	theirs := b.armStatusResponseWait(2, 0x10, false)
	b.endSessionChunkWaits(1)
	if _, open := <-mine; open {
		t.Fatal("session 1's wait was not ended")
	}
	if _, armed := b.routing.statusResponseWaits.Load(mrp.ExchangeKey{SessionID: 2, ExchangeID: 0x10}); !armed {
		t.Fatal("session 2's wait was ended with session 1's")
	}
	select {
	case <-theirs:
		t.Fatal("session 2's wait channel was closed")
	default:
	}
}
