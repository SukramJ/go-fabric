// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// endNotingConn is a fakeConn that records the bridge's notes of session
// traffic and of its sessions' end.
type endNotingConn struct {
	fakeConn
	traffic atomic.Int32
	ended   atomic.Int32
}

func (c *endNotingConn) NoteSessionTraffic() { c.traffic.Add(1) }
func (c *endNotingConn) NoteSessionsEnded()  { c.ended.Add(1) }

// TestLastSessionEndTellsTheConnection (R3-bound-3402, a regression of the
// R2-C2-bound-0201 fix): when the last session bound to a connection goes
// while a handshake keeps the connection open, the bridge tells the
// connection that it carries no session any more — so the replies it then
// sends to the peer's unsecured frames do not renew its place at the TCP
// listener's bound. A connection that still carries a session is not told.
func TestLastSessionEndTellsTheConnection(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	c := &endNotingConn{}
	route := b.routeFor(c)
	b.bindSessionRoute(0x0901, route)
	b.bindSessionRoute(0x0902, route)
	b.holdHandshake(route, onExchange(1), false) // a Sigma1 in flight
	b.unbindSessionRoute(0x0901)
	if n := c.ended.Load(); n != 0 {
		t.Fatalf("told %d times with a session still bound, want 0", n)
	}
	b.unbindSessionRoute(0x0902)
	if n := c.ended.Load(); n != 1 {
		t.Fatalf("told %d times after the last session went with a handshake in flight, want 1", n)
	}
}

// TestBusyPaseRequestsRecordOneRingEventPerClaim (R3-bound-3451): while a
// PASE claim is held, every PBKDFParamRequest of another handshake is
// refused as busy; the diagnostics ring records one event for them per
// claim, so a sender's datagram rate cannot push the ring's older events
// (a seeded one here) out. A new claim records its own busy event again.
func TestBusyPaseRequestsRecordOneRingEventPerClaim(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	ring := diagevent.NewRing(16)
	b.AttachDiagnosticEvents(ring)
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "seeded"})
	if b.claimPase(HandshakeKey{InitiatorNodeID: 0x1111, ExchangeID: 1}) != paseClaimTaken {
		t.Fatal("the first claim was not taken")
	}
	busy := func() int {
		n := 0
		for _, e := range b.DiagnosticEvents() {
			if e.Kind == diagevent.KindPairing {
				n++
			}
		}
		return n
	}
	for i := range 300 {
		hdr := &message.Header{MessageCounter: uint32(i + 1), HasSourceNodeID: true, SourceNodeID: 0x2222}
		if err := b.dispatchPase(loopbackSrc(), hdr, scProto(mrp.SCOpcodePBKDFParamRequest, uint16(100+i), false, 0), nil); err != nil {
			t.Fatal(err)
		}
	}
	seeded := false
	for _, e := range b.DiagnosticEvents() {
		seeded = seeded || e.Message == "seeded"
	}
	if !seeded {
		t.Fatal("300 busy PBKDFParamRequests pushed the seeded event out of the ring")
	}
	if n := busy(); n != 1 {
		t.Fatalf("ring holds %d busy events for one claim, want 1", n)
	}
	b.releasePaseInFlight(HandshakeKey{InitiatorNodeID: 0x1111, ExchangeID: 1})
	if b.claimPase(HandshakeKey{InitiatorNodeID: 0x3333, ExchangeID: 2}) != paseClaimTaken {
		t.Fatal("the second claim was not taken")
	}
	hdr := &message.Header{MessageCounter: 1000, HasSourceNodeID: true, SourceNodeID: 0x2222}
	if err := b.dispatchPase(loopbackSrc(), hdr, scProto(mrp.SCOpcodePBKDFParamRequest, 999, false, 0), nil); err != nil {
		t.Fatal(err)
	}
	if n := busy(); n != 2 {
		t.Fatalf("ring holds %d busy events after a busy request of a second claim, want 2", n)
	}
}

// resumedBetweenHandler answers Sigma3 with success, establishing sid; the
// first read of the session it established after that sees sid, every
// later one another session's — as the shared [CaseAdapter] state reads
// once a second peer's resume (ProcessSigma1) ran on the singleton handler
// between the Sigma3 step and the bridge's read-back.
type resumedBetweenHandler struct {
	sigma2ThenSuccessHandler
	sid, other uint16
	reads      *atomic.Int32
}

func (h resumedBetweenHandler) establishedSession() (uint16, bool) {
	if h.reads.Add(1) == 1 {
		return h.sid, true
	}
	return h.other, true
}

// TestSigma3EventNamesTheSessionItsStepEstablished (R3-sec-3404): the
// route binding and the diagnostics event of a completed Sigma3 use the
// session id the Sigma3 step established, not one read back from the
// handler later — a resume of another peer on the singleton handler in
// between would put this Sigma3's peer address under the other session's
// fabric, where that fabric's RetrieveLogs shows it.
func TestSigma3EventNamesTheSessionItsStepEstablished(t *testing.T) {
	t.Parallel()
	const sid, other = 0x4242, 0x5151
	r := newUnsecuredRig(t)
	ring := diagevent.NewRing(16)
	r.b.AttachDiagnosticEvents(ring)
	r.b.mu.Lock()
	r.b.sessions = sessionTable{sid: {3, 0x1234}, other: {5, 0x5678}}
	r.b.mu.Unlock()
	r.b.AttachCaseHandler(resumedBetweenHandler{sid: sid, other: other, reads: new(atomic.Int32)})
	hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5009)
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Sigma1: %v", err)
	}
	hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5009)
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Sigma3: %v", err)
	}
	evs := sessionEvents(ring)
	if len(evs) != 1 {
		t.Fatalf("%d session events, want 1: %+v", len(evs), evs)
	}
	if got := evs[0].Detail[diagevent.DetailFabricIndex]; got != "3" {
		t.Errorf("the Sigma3's event names fabric %q, want the established session's fabric 3", got)
	}
}
