// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// parkedPeer is a handshake handler whose initiator stated the largest
// session intervals the field holds: a reply on its exchange would wait
// 0xFFFFFFFF ms — 49.7 days, before the backoff — for its first
// retransmission. Neither reference clamps what a peer states (matter.js
// packages/protocol/src/session/SessionIntervals.ts:52-53 at db021da0, "SII/SAI
// in the CASE/PASE session-parameter struct are uint32 and accepted
// unbounded"; chip src/protocols/secure_channel/PairingSession.cpp:175-189 at
// 6170af84).
var parkedPeer = paramsCaseHandler{params: &sessionparams.Parameters{
	SessionIdleInterval: 0xFFFFFFFF, SessionActiveInterval: 0xFFFFFFFF,
}}

// unsecuredRig is a started bridge and a UDP peer that sends handshake
// messages on the unsecured session.
type unsecuredRig struct {
	b    *Bridge
	peer *net.UDPConn
}

func newUnsecuredRig(t *testing.T) *unsecuredRig {
	t.Helper()
	b := newStartedBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return &unsecuredRig{b: b, peer: peer}
}

// request is the header pair of a handshake message on exchangeID.
func (r *unsecuredRig) request(opcode uint8, exchangeID uint16) (*message.Header, message.ProtocolHeader) {
	return &message.Header{MessageCounter: uint32(exchangeID) + 1, HasSourceNodeID: true, SourceNodeID: 0x5A5A},
		message.ProtocolHeader{Initiator: true, NeedsAck: true, Opcode: opcode, ExchangeID: exchangeID, ProtocolID: mrp.SecureChannelProtocolID}
}

// replies reads what reached the peer's socket within wait.
func (r *unsecuredRig) replies(t *testing.T, wait time.Duration) int {
	t.Helper()
	n := 0
	buf := make([]byte, 1500)
	for {
		_ = r.peer.SetReadDeadline(time.Now().Add(wait))
		if _, _, err := r.peer.ReadFromUDP(buf); err != nil {
			return n
		}
		n++
	}
}

func (r *unsecuredRig) pending() int { return r.b.outboundReliable.Pending() }

// TestUnsecuredRetransmitEntriesEndWithTheirHandshake: a handshake reply
// waiting for its retransmission on the unsecured session goes when its
// handshake ends — it fails, it times out, its responder is reaped, it
// moves on. connectedhomeip drops an exchange's retransmission entry when
// the exchange is aborted (src/messaging/ExchangeContext.cpp:241-246 and
// :277-287 at 6170af84, `DoClose(true)` → `ClearRetransTable(this)`), which
// is what releasing a pairing session does to an exchange still waiting on
// a response (src/protocols/secure_channel/PairingSession.cpp:285-290). Here
// the entry stayed until its retries were spent — with the intervals of
// parkedPeer, for months (review II, R2-P0-bound-A06).
func TestUnsecuredRetransmitEntriesEndWithTheirHandshake(t *testing.T) {
	t.Parallel()
	reply := func(opcode uint8) func([]byte) (uint8, []byte, error) {
		return func([]byte) (uint8, []byte, error) { return opcode, []byte{0x15, 0x18}, nil }
	}
	refuse := func([]byte) (uint8, []byte, error) { return 0, nil, errors.New("bridge: test: refused") }
	t.Run("a PASE handshake that fails", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		hdr, proto := r.request(mrp.SCOpcodePBKDFParamRequest, 0x5001)
		if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pbkdf_param_req", reply(mrp.SCOpcodePBKDFParamResponse), parkedPeer); err != nil {
			t.Fatalf("handlePase: %v", err)
		}
		if got := r.pending(); got != 1 {
			t.Fatalf("pending after the PBKDFParamResponse = %d, want 1", got)
		}
		hdr, proto = r.request(mrp.SCOpcodePake1, 0x5001)
		if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pake1", refuse, parkedPeer); err == nil {
			t.Fatal("handlePase accepted a refused Pake1")
		}
		if got := r.pending(); got != 0 {
			t.Errorf("pending after the handshake failed = %d, want 0", got)
		}
	})
	t.Run("a CASE handshake that fails", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5002)
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", reply(mrp.SCOpcodeSigma2), parkedPeer); err != nil {
			t.Fatalf("handleCase: %v", err)
		}
		hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5002)
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma3", refuse, parkedPeer); err == nil {
			t.Fatal("handleCase accepted a refused Sigma3")
		}
		if got := r.pending(); got != 0 {
			t.Errorf("pending after the handshake failed = %d, want 0", got)
		}
	})
	t.Run("a CASE responder that is reaped", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5003)
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", reply(mrp.SCOpcodeSigma2), parkedPeer); err != nil {
			t.Fatalf("handleCase: %v", err)
		}
		// What a per-exchange provider's reaper calls for a responder
		// nobody resolved within its TTL.
		r.b.ForgetSigma1Replied(0x5003)
		if got := r.pending(); got != 0 {
			t.Errorf("pending after the responder was reaped = %d, want 0", got)
		}
	})
	t.Run("a PASE handshake whose claim another takes over after the timeout", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		if !r.b.claimPaseInFlight(0x5004) {
			t.Fatal("the first PBKDFParamRequest did not get the slot")
		}
		hdr, proto := r.request(mrp.SCOpcodePBKDFParamRequest, 0x5004)
		if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pbkdf_param_req", reply(mrp.SCOpcodePBKDFParamResponse), parkedPeer); err != nil {
			t.Fatalf("handlePase: %v", err)
		}
		r.b.mu.Lock()
		r.b.paseInFlightSince = r.b.paseInFlightSince.Add(-pasePairingTimeout - time.Second)
		r.b.mu.Unlock()
		if !r.b.claimPaseInFlight(0x5005) {
			t.Fatal("the slot was not free after the pairing timeout")
		}
		if got := r.pending(); got != 0 {
			t.Errorf("pending of the abandoned handshake = %d, want 0", got)
		}
	})
	t.Run("the timeout, with nobody to take over", func(t *testing.T) {
		t.Parallel()
		if unsecuredEntryLifetime != pasePairingTimeout {
			t.Fatalf("unsecuredEntryLifetime = %v, pasePairingTimeout = %v: a handshake reply's lifetime is the pairing timeout", unsecuredEntryLifetime, pasePairingTimeout)
		}
		tr := newOutboundReliableTracker(nil, nil)
		t0 := time.Unix(1_000_000, 0)
		dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
		tr.trackUnsecured(7, 9, []byte{1}, dest, t0, newHandshakePeerTiming(parkedPeer.params, t0))
		sent := 0
		send := func(net.Addr, []byte) error { sent++; return nil }
		if out := tr.Tick(t0.Add(unsecuredEntryLifetime-time.Second), send); len(out) != 0 || tr.Pending() != 1 {
			t.Fatalf("one second before the timeout: %+v, %d pending — want nothing done, 1 pending", out, tr.Pending())
		}
		out := tr.Tick(t0.Add(unsecuredEntryLifetime+time.Second), send)
		if tr.Pending() != 0 || sent != 0 {
			t.Fatalf("after the timeout: %d pending, %d sent — want the entry gone and nothing sent", tr.Pending(), sent)
		}
		if len(out) != 1 || !errors.Is(out[0].Err, errUnsecuredEntryExpired) || out[0].ExchangeID != 9 {
			t.Errorf("after the timeout: %+v, want the one entry reported expired", out)
		}
	})
	t.Run("Pake3 ends what came before it and keeps its own reply", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		hdr, proto := r.request(mrp.SCOpcodePake1, 0x5006)
		if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pake1", reply(mrp.SCOpcodePake2), parkedPeer); err != nil {
			t.Fatalf("handlePase: %v", err)
		}
		hdr, proto = r.request(mrp.SCOpcodePake3, 0x5006)
		// No PASE handler is wired, so Pake3 is answered by nothing: the
		// unacknowledged Pake2 is all there was.
		if err := r.b.dispatchPase(r.peer.LocalAddr(), hdr, proto, nil); !errors.Is(err, ErrPaseHandlerMissing) {
			t.Fatalf("dispatchPase(Pake3): %v, want ErrPaseHandlerMissing", err)
		}
		if got := r.pending(); got != 0 {
			t.Errorf("pending after Pake3 = %d, want 0", got)
		}
	})
}

// TestUnsecuredRetransmitEntryKeepsItsSchedule is the legitimate path: a
// handshake reply to an ordinary peer is retransmitted on the MRP schedule
// and cleared by the peer's acknowledgement, inside the lifetime.
func TestUnsecuredRetransmitEntryKeepsItsSchedule(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, nil)
	t0 := time.Unix(1_000_000, 0)
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	if !tr.trackUnsecured(7, 9, []byte{1}, dest, t0, newHandshakePeerTiming(nil, t0)) {
		t.Fatal("the reply was not tracked")
	}
	sent := 0
	send := func(net.Addr, []byte) error { sent++; return nil }
	// The active interval's first wait is at most 300 ms × 1.1 × 1.25.
	out := tr.Tick(t0.Add(time.Second), send)
	if sent != 1 || len(out) != 1 || out[0].Err != nil || tr.Pending() != 1 {
		t.Fatalf("after one second: sent %d, %+v, %d pending — want one retransmission, still pending", sent, out, tr.Pending())
	}
	if !tr.Ack(0, 7) || tr.Pending() != 0 {
		t.Fatalf("the acknowledgement did not clear the entry: %d pending", tr.Pending())
	}
}

// TestUnsecuredRetransmitTableIsBounded: the unsecured session holds at
// most unsecuredEntryLimit replies awaiting an acknowledgement, and the
// reply that does not fit is not sent — connectedhomeip's retransmission
// table has CHIP_CONFIG_RMP_RETRANS_TABLE_SIZE entries
// (src/messaging/ReliableMessageMgr.h:267; the size is
// CHIP_CONFIG_MAX_EXCHANGE_CONTEXTS, src/messaging/ReliableMessageProtocolConfig.h:134
// and :146, 16 by default, src/lib/core/CHIPConfig.h:303-305, at 6170af84), a
// full table fails AddToRetransTable with CHIP_ERROR_RETRANS_TABLE_FULL
// (ReliableMessageMgr.cpp:254-263) and the message is not prepared or sent
// (ExchangeMessageDispatch.cpp:63-64). Entries of secure sessions are not
// counted: their peers are authenticated.
func TestUnsecuredRetransmitTableIsBounded(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	sigma2 := func([]byte) (uint8, []byte, error) { return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil }
	for i := range unsecuredEntryLimit {
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x6000+uint16(i))
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer); err != nil {
			t.Fatalf("handleCase %d: %v", i, err)
		}
	}
	if got := r.replies(t, 200*time.Millisecond); got != unsecuredEntryLimit {
		t.Fatalf("%d replies reached the peer, want %d", got, unsecuredEntryLimit)
	}
	hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x6100)
	err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer)
	if !errors.Is(err, errRetransmitTableFull) {
		t.Errorf("the reply past the bound: %v, want errRetransmitTableFull", err)
	}
	if got := r.replies(t, 200*time.Millisecond); got != 0 {
		t.Errorf("%d datagrams reached the peer for the reply that did not fit, want 0", got)
	}
	if got := r.pending(); got != unsecuredEntryLimit {
		t.Errorf("pending = %d, want %d", got, unsecuredEntryLimit)
	}
	// A secure session's message is tracked beside a full unsecured table.
	r.b.outboundReliable.Track(1, 5, 0x7000, []byte{1}, r.peer.LocalAddr(), time.Now())
	if got := r.pending(); got != unsecuredEntryLimit+1 {
		t.Errorf("pending with a secure session's entry = %d, want %d", got, unsecuredEntryLimit+1)
	}
	// One handshake ending makes room for the next.
	if cleared := r.b.outboundReliable.AbandonUnsecuredExchange(0x6000); cleared != 1 {
		t.Fatalf("AbandonUnsecuredExchange cleared %d entries, want 1", cleared)
	}
	hdr, proto = r.request(mrp.SCOpcodeSigma1, 0x6101)
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer); err != nil {
		t.Errorf("the reply after one handshake ended: %v", err)
	}
	// And the secure session's entry is not an unsecured exchange's.
	if cleared := r.b.outboundReliable.AbandonUnsecuredExchange(0x7000); cleared != 0 {
		t.Errorf("AbandonUnsecuredExchange cleared %d entries of a secure session", cleared)
	}
}

// TestSigma1FloodDoesNotGrowTheRetransmitTable sends what a peer without
// any credential can: well-formed Sigma1 messages for a fabric this node
// does not hold, each on an exchange of its own and each stating the
// largest session intervals. The responder refuses every one with a
// StatusReport, which is a reply like any other and waits for its
// acknowledgement; before the bound, 500 of them stayed in the table until
// the process ended (the first retransmission due in 54 days).
func TestSigma1FloodDoesNotGrowTheRetransmitTable(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // SA1019: test fixture
	ours := newCaseTestIdentity(t, 0xBBBB, 1, newCaseTestIPK(t))
	const flood = 500
	refused := 0
	for i := range flood {
		// A stranger: an identity under an IPK this node does not have.
		stranger, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity:      newCaseTestIdentity(t, 0xAAAA, 2, newCaseTestIPK(t)),
			Verifier:      caseTestVerifier{},
			SessionID:     0x1001,
			PeerNodeID:    0xBBBB,
			RootPublicKey: rootPub,
			SessionParams: parkedPeer.params,
		})
		if err != nil {
			t.Fatalf("NewPeerInitiator: %v", err)
		}
		sigma1, err := stranger.GenerateSigma1()
		if err != nil {
			t.Fatalf("GenerateSigma1: %v", err)
		}
		adapter := NewCaseAdapter(sigma.NewResponder(ours, caseTestVerifier{}, uint16(0x2000+i)))
		hdr, proto := r.request(mrp.SCOpcodeSigma1, uint16(0x8000+i))
		err = r.b.handleCase(r.peer.LocalAddr(), hdr, proto, sigma1, "sigma1", adapter.ProcessSigma1, adapter)
		switch {
		case errors.Is(err, errRetransmitTableFull):
			refused++
		case err != nil:
			t.Fatalf("handleCase %d: %v", i, err)
		}
		if p, ok := adapter.PeerSessionParams(), true; i == 0 && (!ok || p == nil || p.SessionIdleInterval != 0xFFFFFFFF) {
			t.Fatalf("the responder kept %+v of the refused Sigma1, want the stated intervals: the test checks nothing", p)
		}
	}
	if got := r.pending(); got > unsecuredEntryLimit {
		t.Errorf("%d entries pending after %d refused Sigma1, want at most %d", got, flood, unsecuredEntryLimit)
	}
	if refused != flood-unsecuredEntryLimit {
		t.Errorf("%d replies were refused for a full table, want %d", refused, flood-unsecuredEntryLimit)
	}
	// The entries that did fit do not stay either.
	r.b.outboundReliable.Tick(time.Now().Add(unsecuredEntryLifetime+time.Second), func(net.Addr, []byte) error { return nil })
	if got := r.pending(); got != 0 {
		t.Errorf("%d entries pending after the handshake timeout, want 0", got)
	}
}

// cachedSigma2Handler is a CASE handler that answers every Sigma1 with the
// same Sigma2, as a responder answers the retransmission of the Sigma1 it
// already processed.
type cachedSigma2Handler struct{ noopCaseHandler }

func (cachedSigma2Handler) ProcessSigma1([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil
}

// TestSigma1RefusedForAFullTableIsAnsweredOnItsRetransmission: a Sigma1
// whose Sigma2 did not fit the table was not answered, so the peer's
// retransmission of it is not dropped as the replay of an answered one —
// it gets its Sigma2 once a handshake has ended and made room.
func TestSigma1RefusedForAFullTableIsAnsweredOnItsRetransmission(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	r.b.AttachCaseHandler(cachedSigma2Handler{})
	now := time.Now()
	for i := range unsecuredEntryLimit {
		if !r.b.outboundReliable.trackUnsecured(uint32(1000+i), uint16(0x6000+i), []byte{1}, r.peer.LocalAddr(), now, nil) {
			t.Fatalf("entry %d did not fit", i)
		}
	}
	hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x6200)
	sigma1 := []byte{0x15, 0x30, 0x01, 0x00, 0x18}
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, sigma1); !errors.Is(err, errRetransmitTableFull) {
		t.Fatalf("Sigma1 at a full table: %v, want errRetransmitTableFull", err)
	}
	if got := r.replies(t, 200*time.Millisecond); got != 0 {
		t.Fatalf("%d datagrams reached the peer, want 0", got)
	}
	r.b.outboundReliable.AbandonUnsecuredExchange(0x6000)
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, sigma1); err != nil {
		t.Fatalf("the retransmitted Sigma1: %v", err)
	}
	if got := r.replies(t, 200*time.Millisecond); got != 1 {
		t.Errorf("%d datagrams reached the peer for the retransmitted Sigma1, want its Sigma2", got)
	}
}
