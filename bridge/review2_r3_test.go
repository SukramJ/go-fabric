// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/im"
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

// TestRemovedFabricsEventsAreNotServedToItsIndexsNextHolder
// (R3-sec-3403, decision 578): a ring event keeps the fabric index of the
// session it names. When that fabric is removed and its index is later
// given to another fabric, the next holder's DiagnosticLogs read must not
// count the removed fabric's events as its own — the node's reader
// (node/node.go ringLogs) serves peer and session_id for an event whose
// fabric_index equals the accessing fabric's. After the removal no held
// event carries the removed index; another fabric's event keeps its own.
func TestRemovedFabricsEventsAreNotServedToItsIndexsNextHolder(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	ring := diagevent.NewRing(16)
	b.AttachDiagnosticEvents(ring)
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "fabric A's session", Detail: map[string]string{
		diagevent.DetailPeer: "[fd00::a]:5540", diagevent.DetailSessionID: "17", diagevent.DetailFabricIndex: "2",
	}})
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "fabric C's session", Detail: map[string]string{
		diagevent.DetailPeer: "[fd00::c]:5540", diagevent.DetailSessionID: "18", diagevent.DetailFabricIndex: "3",
	}})

	b.EmitFabricRemoved(2) // fabric A goes; index 2 is free for reuse

	// Fabric B is given index 2 and records its own session.
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "fabric B's session", Detail: map[string]string{
		diagevent.DetailPeer: "[fd00::b]:5540", diagevent.DetailSessionID: "19", diagevent.DetailFabricIndex: "2",
	}})

	var own []string
	keptC := false
	for _, e := range ring.Snapshot() {
		if e.Detail[diagevent.DetailFabricIndex] == "2" {
			own = append(own, e.Message+" peer="+e.Detail[diagevent.DetailPeer])
		}
		if e.Message == "fabric C's session" && e.Detail[diagevent.DetailFabricIndex] == "3" {
			keptC = true
		}
	}
	if len(own) != 1 || own[0] != "fabric B's session peer=[fd00::b]:5540" {
		t.Fatalf("fabric B (index 2) reads as its own %q, want only its own session", own)
	}
	if !keptC {
		t.Fatal("removing fabric 2 changed fabric 3's event")
	}
}

// TestFabricEventsRecordedBeforeTheDeferredCloseLeaveTheRing (R3-sec-3403,
// decision 578): inside RemoveFabric the fabric's sessions close only
// once the response is out, so an event of one of them — the peer's own
// CloseSession, a TCP drop — can still be recorded with the removed index
// after emitFabricRemoved ran. The close forgets the index again.
func TestFabricEventsRecordedBeforeTheDeferredCloseLeaveTheRing(t *testing.T) {
	t.Parallel()
	b := newUnstartedBridge(t)
	reg := &fabricClosingRegistry{}
	b.AttachSessionRegistry(reg)
	ring := diagevent.NewRing(8)
	b.AttachDiagnosticEvents(ring)

	after := &im.AfterResponse{}
	b.EmitFabricRemovedContext(im.WithAfterResponse(context.Background(), after), 2)
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "closed before the response went", Detail: map[string]string{
		diagevent.DetailPeer: "[fd00::a]:5540", diagevent.DetailFabricIndex: "2",
	}})
	after.Run()
	for _, e := range ring.Snapshot() {
		if e.Detail[diagevent.DetailFabricIndex] == "2" {
			t.Fatalf("event %q still names the removed fabric after its sessions closed", e.Message)
		}
	}
}
