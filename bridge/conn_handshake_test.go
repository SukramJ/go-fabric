// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// handshakeCaseHandler answers Sigma1 with a Sigma2 and Sigma3 with the
// success StatusReport, and then names sessionID as established — the
// shape of [CaseAdapter] without the cryptography.
type handshakeCaseHandler struct {
	mu          sync.Mutex
	sessionID   uint16
	established bool
}

func (h *handshakeCaseHandler) ProcessSigma1([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil
}

func (h *handshakeCaseHandler) ProcessSigma3([]byte) (opcode uint8, payload []byte, err error) {
	h.mu.Lock()
	h.established = true
	h.mu.Unlock()
	return mrp.SCOpcodeStatusReport, mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess,
		uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil), nil
}

func (h *handshakeCaseHandler) ProcessSigma2Resume([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errors.New("not a resume")
}

func (h *handshakeCaseHandler) establishedSession() (uint16, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionID, h.established
}

// unsecuredSC is an unsecured Secure-Channel message from the initiator.
func unsecuredSC(opcode uint8, xid uint16, counter uint32) []byte {
	hdr := message.Header{SessionID: 0, MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: 0x5A5A}
	proto := message.ProtocolHeader{Initiator: true, Opcode: opcode, ExchangeID: xid, ProtocolID: mrp.SecureChannelProtocolID}
	return append(append(hdr.Marshal(), proto.Marshal()...), 0x15, 0x18)
}

// TestTCPHandshakeHoldsConnection: the last secure session on a TCP
// connection closes while a CASE handshake is in flight on it — the
// connection stays, the handshake completes and its session binds to the
// connection, and the connection closes when that session goes. matter.js
// counts the unsecured session on the channel (ExchangeManager.ts:634-648,
// :644-645) and creates the secure session on it (CaseServer.ts:316-317).
func TestTCPHandshakeHoldsConnection(t *testing.T) {
	h := newTCPHarness(t)
	const established = harnessLocalSessionID + 7
	ch := &handshakeCaseHandler{sessionID: established}
	h.bridge.AttachCaseHandler(ch)
	conn, replies := h.dial()
	if err := conn.Send(h.sealedInvoke(0x320)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, replies)

	const xid = 0x0321
	if err := conn.Send(unsecuredSC(mrp.SCOpcodeSigma1, xid, 1)); err != nil {
		t.Fatal(err)
	}
	if got := scOpcodeOf(t, waitReply(t, replies)); got != mrp.SCOpcodeSigma2 {
		t.Fatalf("Sigma1 answered with opcode %#x", got)
	}

	h.bridge.SessionClosed(harnessLocalSessionID)
	assertConnOpen(t, conn, "its last secure session closed while a handshake was in flight")

	if err := conn.Send(unsecuredSC(mrp.SCOpcodeSigma3, xid, 2)); err != nil {
		t.Fatal(err)
	}
	if got := scOpcodeOf(t, waitReply(t, replies)); got != mrp.SCOpcodeStatusReport {
		t.Fatalf("Sigma3 answered with opcode %#x", got)
	}
	raw, ok := h.bridge.sessionPeerAddrs.Load(established)
	route, _ := raw.(*connRoute)
	if !ok || route == nil {
		t.Fatalf("the established session is bound to %v, want the connection", raw)
	}
	h.bridge.routeBindMu.Lock()
	sessions, handshakes := route.sessions, len(route.handshakes)
	h.bridge.routeBindMu.Unlock()
	if sessions != 1 || handshakes != 0 {
		t.Fatalf("connection counts %d sessions and %d handshakes, want 1 and 0", sessions, handshakes)
	}
	assertConnOpen(t, conn, "the handshake completed")

	h.bridge.SessionClosed(established)
	select {
	case <-conn.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("connection still open after the handshake's session went")
	}
}

func assertConnOpen(t *testing.T, conn *tcp.Conn, when string) {
	t.Helper()
	select {
	case <-conn.Closed():
		t.Fatalf("connection closed after %s", when)
	case <-time.After(100 * time.Millisecond):
	}
}

// scOpcodeOf is the opcode of an unsecured message the bridge sent.
func scOpcodeOf(t *testing.T, msg []byte) uint8 {
	t.Helper()
	_, n, err := message.UnmarshalHeader(msg)
	if err != nil {
		t.Fatal(err)
	}
	proto, _, err := message.UnmarshalProtocolHeader(msg[n:])
	if err != nil {
		t.Fatal(err)
	}
	return proto.Opcode
}

// TestHandshakeHoldLifecycle: a hold ends when the handshake fails (an
// error or a failure StatusReport from the handler), when the peer ends
// it with a failure StatusReport, when its CASE state is evicted, and —
// for PASE — after the pairing timeout; a replayed step and a step that
// continues the handshake keep it, and nothing of this is tracked over
// UDP. While a hold is live the connection's last session does not close
// it; a hold ending closes nothing.
func TestHandshakeHoldLifecycle(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	c := &fakeConn{}
	route := b.routeFor(c)
	r, ok := route.(*connRoute)
	if !ok {
		t.Fatalf("route %T", route)
	}
	held := func() int {
		b.routeBindMu.Lock()
		defer b.routeBindMu.Unlock()
		return r.liveHandshakesLocked(time.Now())
	}
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)

	b.holdHandshake(route, 1, false)
	b.holdHandshake(route, 1, false) // a retransmitted Sigma1 counts once
	b.settleHandshake(route, 1, nil, mrp.SCOpcodeSigma2, []byte{1}, nil)
	b.settleHandshake(route, 1, nil, 0, nil, sigma.ErrSessionState)
	b.settleHandshake(route, 1, nil, 0, nil, spake2.ErrSessionState)
	if n := held(); n != 1 {
		t.Fatalf("%d holds after a continuing and two replayed steps, want 1", n)
	}
	b.settleHandshake(route, 1, nil, mrp.SCOpcodeStatusReport, failure, nil)
	b.holdHandshake(route, 2, false)
	b.settleHandshake(route, 2, nil, 0, nil, errors.New("decode"))
	b.holdHandshake(route, 3, false)
	b.ForgetSigma1Replied(3)
	if n := held(); n != 0 {
		t.Fatalf("%d holds after failures and an eviction, want 0", n)
	}

	// The peer's failure StatusReport on the handshake's exchange.
	b.holdHandshake(route, 4, true)
	hdr := &message.Header{SessionID: 0, MessageCounter: 9}
	if err := b.dispatchSecureChannel(route, hdr, scProto(mrp.SCOpcodeStatusReport, 4, false, 0), failure); err != nil {
		t.Fatal(err)
	}
	if n := held(); n != 0 {
		t.Fatalf("%d holds after the peer's failure report, want 0", n)
	}

	// A resume establishes at once; without a named session the hold
	// just ends.
	b.holdHandshake(route, 5, false)
	b.settleHandshake(route, 5, nil, mrp.SCOpcodeSigma2Resume, []byte{1}, nil)
	if n := held(); n != 0 {
		t.Fatalf("%d holds after a resume, want 0", n)
	}

	// A PASE hold past the pairing timeout no longer counts; a CASE one
	// still does.
	b.routeBindMu.Lock()
	r.handshakes = map[uint16]handshakeHold{
		6: {pase: true, since: time.Now().Add(-pasePairingTimeout - time.Second)},
		7: {pase: false, since: time.Now().Add(-pasePairingTimeout - time.Second)},
	}
	b.routeBindMu.Unlock()
	if n := held(); n != 1 {
		t.Fatalf("%d live holds, want the CASE one", n)
	}

	// The last session goes while the CASE hold is live: the route stays.
	b.bindSessionRoute(0x0901, route)
	b.unbindSessionRoute(0x0901)
	b.routeBindMu.Lock()
	sessions := r.sessions
	b.routeBindMu.Unlock()
	if sessions != 0 {
		t.Fatalf("route counts %d sessions", sessions)
	}

	// Over UDP nothing is held.
	udp := loopbackSrc()
	b.holdHandshake(udp, 8, false)
	b.settleHandshake(udp, 8, nil, mrp.SCOpcodeStatusReport, failure, nil)
	b.releaseHandshake(udp, 8)
	b.releaseHandshake(nil, 8)
}

// TestAdaptersNameTheirEstablishedSession: the CASE and PASE adapters name
// the session their handshake established only once it is.
func TestAdaptersNameTheirEstablishedSession(t *testing.T) {
	t.Parallel()
	ca := &CaseAdapter{}
	if _, ok := ca.establishedSession(); ok {
		t.Fatal("CASE adapter established before a handshake")
	}
	ca.established, ca.establishedSessionID = true, 0x44
	if sid, ok := ca.establishedSession(); !ok || sid != 0x44 {
		t.Fatalf("CASE adapter established %#x %v", sid, ok)
	}
	pa := &PaseAdapter{}
	pa.SetPBKDFParams(1000, []byte("salt"), 0x55)
	if _, ok := pa.establishedSession(); ok {
		t.Fatal("PASE adapter established before Pake3")
	}
	pa.established = true
	if sid, ok := pa.establishedSession(); !ok || sid != 0x55 {
		t.Fatalf("PASE adapter established %#x %v", sid, ok)
	}
}
