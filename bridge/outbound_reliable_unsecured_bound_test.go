// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"testing/synctest"
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

func (r *unsecuredRig) pending() int { return r.b.outboundReliable.Pending() }

// next reads the bridge's next message to the peer and returns its
// protocol header; standalone acknowledgements are skipped.
func (r *unsecuredRig) next(t *testing.T, what string) message.ProtocolHeader {
	t.Helper()
	_, proto := r.nextMessage(t, what)
	return proto
}

// nextMessage is next with the message header.
func (r *unsecuredRig) nextMessage(t *testing.T, what string) (message.Header, message.ProtocolHeader) {
	t.Helper()
	buf := make([]byte, 1500)
	for {
		_ = r.peer.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := r.peer.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("%s: nothing reached the peer: %v", what, err)
		}
		hdr, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("%s: header: %v", what, err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			t.Fatalf("%s: protocol header: %v", what, err)
		}
		if proto.Opcode != mrp.StandaloneAckOpcode {
			return hdr, proto
		}
	}
}

// tracked returns the opcodes of the unsecured session's entries of
// exchangeID that wait for an acknowledgement.
func (r *unsecuredRig) tracked(t *testing.T, exchangeID uint16) []uint8 {
	t.Helper()
	tr := r.b.outboundReliable
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var opcodes []uint8
	for key, e := range tr.pending {
		if key.sessionID != 0 || e.exchangeID != exchangeID {
			continue
		}
		_, hl, err := message.UnmarshalHeader(e.datagram)
		if err != nil {
			t.Fatalf("tracked datagram: %v", err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(e.datagram[hl:])
		if err != nil {
			t.Fatalf("tracked datagram: %v", err)
		}
		opcodes = append(opcodes, proto.Opcode)
	}
	return opcodes
}

// wantOnlyTheStatusReport asserts that the handshake on exchangeID holds
// one entry, its StatusReport, and that the report on the wire asks for an
// acknowledgement.
func (r *unsecuredRig) wantOnlyTheStatusReport(t *testing.T, exchangeID uint16) {
	t.Helper()
	var onWire message.ProtocolHeader
	for onWire.Opcode != mrp.SCOpcodeStatusReport {
		onWire = r.next(t, "the StatusReport")
	}
	if !onWire.NeedsAck {
		t.Errorf("the StatusReport went out without the R flag: it is sent once and never again")
	}
	if got := r.tracked(t, exchangeID); len(got) != 1 || got[0] != mrp.SCOpcodeStatusReport {
		t.Errorf("entries of the exchange after its handshake ended: opcodes %#x, want the StatusReport alone (%#x)", got, mrp.SCOpcodeStatusReport)
	}
}

// TestUnsecuredRetransmitEntriesEndWithTheirHandshake: a handshake reply
// waiting for its retransmission on the unsecured session goes when its
// handshake ends — it fails, it times out, its responder is reaped, it
// moves on — and the StatusReport that ends the handshake stays, because it
// is sent after the rest was dropped. connectedhomeip drops an exchange's
// retransmission entry when the exchange is aborted
// (src/messaging/ExchangeContext.cpp:241-246 and :277-287 at 6170af84,
// `DoClose(true)` → `ClearRetransTable(this)`), and releasing a pairing
// session aborts its exchange only while that still waits on a response —
// "otherwise the exchange layer finishes closing it"
// (src/protocols/secure_channel/PairingSession.cpp:285-290), which is the
// StatusReport's case. matter.js sends the report as a reliable message and
// waits for its acknowledgement
// (packages/protocol/src/securechannel/SecureChannelMessenger.ts:209-232,
// packages/protocol/src/protocol/MessageExchange.ts:692, :785-790 and
// :821-828 at db021da0). Before review II an entry stayed until its retries
// were spent — with the intervals of parkedPeer, for months
// (R2-P0-bound-A06); then the failure report went out without the R flag
// and the success report after Sigma3 was dropped right after it was
// tracked (R2-P0-rv-A04, F-db021da0-5).
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
		// The PBKDFParamResponse is gone, the failure report is not.
		r.wantOnlyTheStatusReport(t, 0x5001)
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
		// Sigma2 is gone, the failure report is not.
		r.wantOnlyTheStatusReport(t, 0x5002)
	})
	t.Run("a CASE handshake that completes", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		r.b.AttachCaseHandler(sigma2ThenSuccessHandler{})
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5007)
		if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("Sigma1: %v", err)
		}
		if got := r.tracked(t, 0x5007); len(got) != 1 || got[0] != mrp.SCOpcodeSigma2 {
			t.Fatalf("entries after Sigma1: opcodes %#x, want Sigma2", got)
		}
		hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5007)
		if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("Sigma3: %v", err)
		}
		// Sigma2 is gone, the success report waits for its acknowledgement.
		r.wantOnlyTheStatusReport(t, 0x5007)
	})
	t.Run("a CASE handshake the shipped adapter refuses at Sigma3", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		// [CaseAdapter] hands a refusal back as a reply, not as an error.
		r.b.AttachCaseHandler(sigma2ThenRefusalHandler{})
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5008)
		if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("Sigma1: %v", err)
		}
		hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5008)
		if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("Sigma3: %v", err)
		}
		r.wantOnlyTheStatusReport(t, 0x5008)
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
	tr.trackUnsecured(7, 9, []byte{1}, dest, t0, newHandshakePeerTiming(nil, t0))
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

// sigma2ThenSuccessHandler answers Sigma1 with a Sigma2 and Sigma3 with
// the success StatusReport, as [CaseAdapter] does for a handshake that
// verifies.
type sigma2ThenSuccessHandler struct{ noopCaseHandler }

func (sigma2ThenSuccessHandler) ProcessSigma1([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil
}

func (sigma2ThenSuccessHandler) ProcessSigma3([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodeStatusReport, mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil), nil
}

// sigma2ThenRefusalHandler answers Sigma3 with the failure StatusReport as
// a reply, as [CaseAdapter.ProcessSigma3] does for one that does not verify.
type sigma2ThenRefusalHandler struct{ sigma2ThenSuccessHandler }

func (sigma2ThenRefusalHandler) ProcessSigma3([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodeStatusReport, mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil), nil
}

// TestUnsecuredRetransmitTableEvictsItsOldestEntry is the table's rule
// (review II, owner decision 23): the unsecured session holds at most
// unsecuredEntryLimit messages awaiting an acknowledgement, a message that
// arrives at a full table is sent and tracked all the same, and the entry
// tracked first loses its place — its retransmissions, nothing else.
// Entries of secure sessions are neither counted nor evicted: their peers
// are authenticated. The rule is this module's own; the bound it replaces
// refused the new reply (R2-P0-rv-A01).
func TestUnsecuredRetransmitTableEvictsItsOldestEntry(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	type eviction struct {
		exchangeID uint16
		counter    uint32
	}
	var evicted []eviction
	r.b.outboundReliable.evicted = func(exchangeID uint16, counter uint32) {
		evicted = append(evicted, eviction{exchangeID, counter})
	}
	// A secure session's message, tracked before everything else.
	r.b.outboundReliable.Track(1, 5, 0x6000, []byte{1}, r.peer.LocalAddr(), time.Now())
	sigma2 := func([]byte) (uint8, []byte, error) { return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil }
	for i := range unsecuredEntryLimit {
		hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x6000+uint16(i))
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer); err != nil {
			t.Fatalf("handleCase %d: %v", i, err)
		}
		r.next(t, "a Sigma2 below the bound")
	}
	if got := r.pending(); got != unsecuredEntryLimit+1 || len(evicted) != 0 {
		t.Fatalf("a full table: %d pending, %d evicted — want %d and none", got, len(evicted), unsecuredEntryLimit+1)
	}
	// The seventeenth reply is sent …
	hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x6100)
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer); err != nil {
		t.Fatalf("the reply past the bound: %v", err)
	}
	if got := r.next(t, "the reply past the bound"); got.Opcode != mrp.SCOpcodeSigma2 || got.ExchangeID != 0x6100 || !got.NeedsAck {
		t.Errorf("past the bound the peer got opcode %#x on exchange %#x, needs ack %v — want the Sigma2 of exchange 0x6100 with the R flag", got.Opcode, got.ExchangeID, got.NeedsAck)
	}
	// … and tracked, in the place of the entry tracked first.
	if got := r.tracked(t, 0x6100); len(got) != 1 {
		t.Errorf("the reply past the bound has %d entries, want 1", len(got))
	}
	if got := r.tracked(t, 0x6000); len(got) != 0 {
		t.Errorf("the oldest entry is still tracked (%d entries)", len(got))
	}
	for i := 1; i < unsecuredEntryLimit; i++ {
		if got := r.tracked(t, 0x6000+uint16(i)); len(got) != 1 {
			t.Errorf("exchange %#x has %d entries, want 1: only the oldest loses its place", 0x6000+i, len(got))
		}
	}
	if len(evicted) != 1 || evicted[0].exchangeID != 0x6000 {
		t.Errorf("evictions reported: %+v, want the one of exchange 0x6000", evicted)
	}
	if got := r.pending(); got != unsecuredEntryLimit+1 {
		t.Errorf("pending = %d, want %d unsecured and the secure session's one", got, unsecuredEntryLimit)
	}
	// The secure session's entry, older than all of them, is still there,
	// and is no unsecured exchange's.
	if cleared := r.b.outboundReliable.AbandonUnsecuredExchange(0x6000); cleared != 0 {
		t.Errorf("AbandonUnsecuredExchange cleared %d entries of a secure session", cleared)
	}
	if !r.b.outboundReliable.Ack(5, 1) {
		t.Error("the secure session's entry was evicted by the unsecured session's")
	}
	// A handshake that ends makes room: the next reply evicts nothing.
	if cleared := r.b.outboundReliable.AbandonUnsecuredExchange(0x6001); cleared != 1 {
		t.Fatalf("AbandonUnsecuredExchange cleared %d entries, want 1", cleared)
	}
	hdr, proto = r.request(mrp.SCOpcodeSigma1, 0x6101)
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", sigma2, parkedPeer); err != nil {
		t.Errorf("the reply after one handshake ended: %v", err)
	}
	if len(evicted) != 1 {
		t.Errorf("%d evictions after a handshake ended and one reply followed, want still 1", len(evicted))
	}
}

// TestUnsecuredRetransmitEvictionWakesItsWaiter: a caller waiting for the
// acknowledgement of an entry that is evicted learns so at once.
func TestUnsecuredRetransmitEvictionWakesItsWaiter(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		tr := newOutboundReliableTracker(nil, nil)
		t0 := time.Now()
		dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
		tr.trackUnsecured(1, 0x10, []byte{1}, dest, t0, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- tr.WaitForAck(ctx, 0, 1) }()
		synctest.Wait()
		for i := range unsecuredEntryLimit {
			tr.trackUnsecured(uint32(2+i), uint16(0x11+i), []byte{1}, dest, t0, nil)
		}
		synctest.Wait()
		select {
		case err := <-result:
			if !errors.Is(err, errUnsecuredEntryEvicted) {
				t.Errorf("the waiter of the evicted entry got %v, want errUnsecuredEntryEvicted", err)
			}
		default:
			t.Error("the waiter of the evicted entry is still waiting")
		}
		if got := tr.Pending(); got != unsecuredEntryLimit {
			t.Errorf("pending = %d, want %d", got, unsecuredEntryLimit)
		}
	})
}

// TestInitiatedHandshakeIsTrackedAtAFullTable: a message of a handshake
// this node opens shares the unsecured session's table, and at a full table
// it is tracked like a reply — the oldest entry goes. Under the bound this
// replaces it was sent with the R flag and not tracked, without an error
// (review II, R2-P0-rv-A05).
func TestInitiatedHandshakeIsTrackedAtAFullTable(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	now := time.Now()
	for i := range unsecuredEntryLimit {
		r.b.outboundReliable.trackUnsecured(uint32(1000+i), uint16(0x6000+i), []byte{1}, r.peer.LocalAddr(), now, nil)
	}
	peerAddr, _ := r.peer.LocalAddr().(*net.UDPAddr)
	if err := r.b.sendInitiatedUnsecured(peerAddr, 0x0042, 0xE0E0, mrp.SCOpcodeSigma1, []byte{0x15, 0x18}, nil); err != nil {
		t.Fatalf("sendInitiatedUnsecured: %v", err)
	}
	if got := r.next(t, "the Sigma1 this node sends"); got.Opcode != mrp.SCOpcodeSigma1 || !got.NeedsAck {
		t.Fatalf("the peer got opcode %#x, needs ack %v — want Sigma1 with the R flag", got.Opcode, got.NeedsAck)
	}
	if got := r.tracked(t, 0x0042); len(got) != 1 || got[0] != mrp.SCOpcodeSigma1 {
		t.Errorf("the Sigma1 sent with the R flag at a full table: entries %#x, want it tracked", got)
	}
	if got := r.pending(); got != unsecuredEntryLimit {
		t.Errorf("pending = %d, want %d", got, unsecuredEntryLimit)
	}
}

// TestInitiatedHandshakeKeepsItsFailureReportTracked: a handshake this node
// opened and has to give up — the peer's Sigma2 does not verify — tells the
// peer so with a StatusReport, and that report is retransmitted until it is
// acknowledged: what the handshake sent before is dropped first, the report
// stays. matter.js CaseClient sends it through the same `sendError` as the
// responder (packages/protocol/src/securechannel/SecureChannelMessenger.ts:170-172
// and :209-232 at db021da0). Before review II the handshake's end dropped
// every entry of the exchange, the report it had just tracked included.
func TestInitiatedHandshakeKeepsItsFailureReportTracked(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity:      newCaseTestIdentity(t, 0xBBBB, 1, newCaseTestIPK(t)),
		Verifier:      caseTestVerifier{},
		SessionID:     0x1003,
		PeerNodeID:    0xAAAA,
		RootPublicKey: elliptic.Marshal(elliptic.P256(), root.X, root.Y), //nolint:staticcheck // SA1019: test fixture
	})
	if err != nil {
		t.Fatalf("NewPeerInitiator: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	peerAddr, _ := r.peer.LocalAddr().(*net.UDPAddr)
	done := make(chan error, 1)
	go func() {
		_, err := r.b.runCASEInitiator(ctx, peerAddr, &CaseInitiation{Initiator: initiator})
		done <- err
	}()
	sigma1Hdr, sigma1 := r.nextMessage(t, "the Sigma1 this node sends")
	if sigma1.Opcode != mrp.SCOpcodeSigma1 {
		t.Fatalf("the peer got opcode %#x, want Sigma1", sigma1.Opcode)
	}
	// The peer answers with a Sigma2 that is none.
	hdr := message.Header{MessageCounter: 77, DestSize: message.DestNodeID, DestNodeID: sigma1Hdr.SourceNodeID}
	proto := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeSigma2, ExchangeID: sigma1.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck: true, HasAck: true, AckCounter: sigma1Hdr.MessageCounter,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	datagram = append(datagram, 0xFF)
	if err := r.b.dispatch(ctx, datagram, r.peer.LocalAddr()); err != nil {
		t.Fatalf("dispatch(Sigma2): %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the handshake accepted a Sigma2 that does not decode")
		}
	case <-ctx.Done():
		t.Fatal("the handshake did not end")
	}
	r.wantOnlyTheStatusReport(t, sigma1.ExchangeID)
}

// TestSigma1FloodDoesNotGrowTheRetransmitTable sends what a peer without
// any credential can: well-formed Sigma1 messages, each on an exchange of
// its own and each stating the largest session intervals. Every one is
// answered, the answer waits for its acknowledgement, and before the bound
// 500 of them stayed in the table until the process ended (the first
// retransmission due in 54 days). The same flood through the receive
// pipeline, around two handshakes that have to complete, is
// TestSigma1FloodDoesNotStopLegitimateHandshakes.
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
		// Every reply is sent: none is refused for a full table.
		if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, sigma1, "sigma1", adapter.ProcessSigma1, adapter); err != nil {
			t.Fatalf("handleCase %d: %v", i, err)
		}
		if p, ok := adapter.PeerSessionParams(), true; i == 0 && (!ok || p == nil || p.SessionIdleInterval != 0xFFFFFFFF) {
			t.Fatalf("the responder kept %+v of the Sigma1, want the stated intervals: the test checks nothing", p)
		}
	}
	if got := r.pending(); got != unsecuredEntryLimit {
		t.Errorf("%d entries pending after %d Sigma1, want %d", got, flood, unsecuredEntryLimit)
	}
	// The newest are the ones that stay.
	if got := r.tracked(t, uint16(0x8000+flood-1)); len(got) != 1 {
		t.Errorf("the last reply of the flood has %d entries, want 1", len(got))
	}
	if got := r.tracked(t, 0x8000); len(got) != 0 {
		t.Errorf("the first reply of the flood is still tracked")
	}
	// And they do not stay either.
	r.b.outboundReliable.Tick(time.Now().Add(unsecuredEntryLifetime+time.Second), func(net.Addr, []byte) error { return nil })
	if got := r.pending(); got != 0 {
		t.Errorf("%d entries pending after the lifetime, want 0", got)
	}
}
