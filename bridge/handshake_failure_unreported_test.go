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
	"time"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// wantUnreportedRetransmitting asserts that no StatusReport reached the
// peer — a report is sent synchronously with the failing message's
// handling, so it is in the peer's socket already when it was sent at all —
// and that the handshake's unacknowledged reply (opcode want) is still
// tracked for retransmission (owner decision 173); then the peer's
// acknowledgement ends it.
func (r *unsecuredRig) wantUnreportedRetransmitting(t *testing.T, key HandshakeKey, want uint8) {
	t.Helper()
	if got := r.tracked(t, key.ExchangeID); len(got) != 1 || got[0] != want {
		t.Errorf("entries of the exchange after its handshake ended unreported: opcodes %#x, want the unacknowledged reply %#x alone", got, want)
	}
	buf := make([]byte, 1500)
	for {
		_ = r.peer.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, from, err := r.peer.ReadFromUDP(buf)
		if err != nil {
			break // nothing more on the wire
		}
		if !unsecuredFromBridge(r.b, from, buf[:n]) {
			continue
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			continue
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err == nil && proto.Opcode == mrp.SCOpcodeStatusReport {
			t.Fatal("a StatusReport reached the peer while the handshake's reply was unacknowledged")
		}
	}
	// The acknowledgement ends the retransmission.
	tr := r.b.outboundReliable
	tr.mu.Lock()
	var counters []uint32
	for k, e := range tr.pending {
		if k.sessionID == 0 && e.exchangeID == key.ExchangeID {
			counters = append(counters, k.counter)
		}
	}
	tr.mu.Unlock()
	for _, c := range counters {
		tr.AckUnsecured(key, c)
	}
	if got := r.tracked(t, key.ExchangeID); len(got) != 0 {
		t.Errorf("entries after the peer acknowledged the reply: opcodes %#x, want none", got)
	}
}

// ackLastReply reads the bridge's reply on the wire and acknowledges it as
// the peer's next message would, on the handshake key of [scKey].
func (r *unsecuredRig) ackLastReply(t *testing.T, exchangeID uint16, want uint8) {
	t.Helper()
	hdr, proto := r.nextMessage(t, "the handshake's reply")
	if proto.Opcode != want {
		t.Fatalf("the peer got opcode %#x, want %#x", proto.Opcode, want)
	}
	if !r.b.outboundReliable.AckUnsecured(scKey(exchangeID), hdr.MessageCounter) {
		t.Fatal("the acknowledgement of the reply cleared nothing")
	}
}

func replyWith(opcode uint8) func([]byte) (uint8, []byte, error) {
	return func([]byte) (uint8, []byte, error) { return opcode, []byte{0x15, 0x18}, nil }
}

func refuseStep([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errors.New("bridge: test: refused")
}

// paseFailsAtPake1 runs PBKDFParamRequest, then a Pake1 the handler refuses.
func paseFailsAtPake1(t *testing.T, r *unsecuredRig, x uint16, acked bool) {
	t.Helper()
	hdr, proto := r.request(mrp.SCOpcodePBKDFParamRequest, x)
	if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pbkdf_param_req", replyWith(mrp.SCOpcodePBKDFParamResponse), parkedPeer); err != nil {
		t.Fatalf("handlePase: %v", err)
	}
	if acked {
		r.ackLastReply(t, x, mrp.SCOpcodePBKDFParamResponse)
	}
	hdr, proto = r.request(mrp.SCOpcodePake1, x)
	if err := r.b.handlePase(r.peer.LocalAddr(), hdr, proto, nil, "pake1", refuseStep, parkedPeer); err == nil {
		t.Fatal("handlePase accepted a refused Pake1")
	}
}

// pake3Refuser answers Pake1 with a Pake2 and refuses Pake3, as a wrong
// passcode fails the key confirmation.
type pake3Refuser struct{ noopPaseHandler }

func (pake3Refuser) ProcessPake1([]byte) (opcode uint8, payload []byte, err error) {
	return mrp.SCOpcodePake2, []byte{0x15, 0x18}, nil
}

func (pake3Refuser) ProcessPake3([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, errors.New("bridge: test: key confirmation failed")
}

// paseFailsAtPake3 runs Pake1, then a Pake3 the handler refuses, through
// the router (dispatchPase), with the PASE claim held as a
// PBKDFParamRequest leaves it. It asserts what a failed Pake3 does
// whatever is reported: one PASE failure counted, the claim released.
func paseFailsAtPake3(t *testing.T, r *unsecuredRig, x uint16, acked bool) {
	t.Helper()
	r.b.AttachPaseHandler(pake3Refuser{})
	hdr, proto := r.request(mrp.SCOpcodePake1, x)
	beginPase(t, r.b, handshakeKeyOf(hdr, x))
	if err := r.b.dispatchPase(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Pake1: %v", err)
	}
	if acked {
		r.ackLastReply(t, x, mrp.SCOpcodePake2)
	}
	before := r.b.paseFailures.Load()
	hdr, proto = r.request(mrp.SCOpcodePake3, x)
	if err := r.b.dispatchPase(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err == nil {
		t.Fatal("dispatchPase accepted a refused Pake3")
	}
	if got := r.b.paseFailures.Load() - before; got != 1 {
		t.Errorf("PASE failures counted for the refused Pake3 = %d, want 1", got)
	}
	if _, begun := r.b.resolvePaseContinuation(handshakeKeyOf(hdr, x)); begun {
		t.Error("the PASE claim is still held after the refused Pake3")
	}
}

// caseFailsAtSigma3 runs Sigma1, then a Sigma3 the handler refuses.
func caseFailsAtSigma3(t *testing.T, r *unsecuredRig, x uint16, acked bool) {
	t.Helper()
	hdr, proto := r.request(mrp.SCOpcodeSigma1, x)
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma1", replyWith(mrp.SCOpcodeSigma2), parkedPeer); err != nil {
		t.Fatalf("handleCase: %v", err)
	}
	if acked {
		r.ackLastReply(t, x, mrp.SCOpcodeSigma2)
	}
	hdr, proto = r.request(mrp.SCOpcodeSigma3, x)
	if err := r.b.handleCase(r.peer.LocalAddr(), hdr, proto, nil, "sigma3", refuseStep, parkedPeer); err == nil {
		t.Fatal("handleCase accepted a refused Sigma3")
	}
}

// caseRefusedAtSigma3 runs Sigma1, then a Sigma3 the handler answers with
// its failure report, as [CaseAdapter] does.
func caseRefusedAtSigma3(t *testing.T, r *unsecuredRig, x uint16, acked bool) {
	t.Helper()
	r.b.AttachCaseHandler(sigma2ThenRefusalHandler{})
	hdr, proto := r.request(mrp.SCOpcodeSigma1, x)
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Sigma1: %v", err)
	}
	if acked {
		r.ackLastReply(t, x, mrp.SCOpcodeSigma2)
	}
	hdr, proto = r.request(mrp.SCOpcodeSigma3, x)
	if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Sigma3: %v", err)
	}
}

// TestHandshakeFailureUnreportedWhileAReplyIsUnacknowledged: a handshake
// that fails on a message arriving while the handshake's previous reply
// still waits for its acknowledgement ends without a failure StatusReport,
// and that reply goes on being retransmitted until the peer acknowledges
// it (owner decision 173). The cases call the handlers below the receive
// rule: since review II, phase 7 (decision 277) such a message is refused
// on receive over UDP, and the guard is the second line for the residue
// (owner decision 282, [Bridge.endHandshakeUnreported]). Once that reply is acknowledged, the same
// failure is reported, reliably (review II, F-db021da0-7; the register row
// in notes/parity/matter_behaviour_findings.md). Before, the reply was
// dropped and the report sent reliably in both cases. One case per role and per
// way a failure leaves the handler: an error, and a failure report as the
// handler's reply.
func TestHandshakeFailureUnreportedWhileAReplyIsUnacknowledged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		run   func(t *testing.T, r *unsecuredRig, x uint16, acked bool)
		reply uint8 // the reply the failing message did not acknowledge
	}{
		{"PASE, the handler refuses Pake1", paseFailsAtPake1, mrp.SCOpcodePBKDFParamResponse},
		{"PASE, the handler refuses Pake3 (through the router)", paseFailsAtPake3, mrp.SCOpcodePake2},
		{"CASE, the handler refuses Sigma3", caseFailsAtSigma3, mrp.SCOpcodeSigma2},
		{"CASE, the shipped adapter answers Sigma3 with its refusal", caseRefusedAtSigma3, mrp.SCOpcodeSigma2},
	}
	for i, c := range cases {
		for _, acked := range []bool{false, true} {
			name := c.name + "/reply unacknowledged"
			if acked {
				name = c.name + "/reply acknowledged"
			}
			x := uint16(0x5100 + 2*i)
			if acked {
				x++
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				r := newUnsecuredRig(t)
				c.run(t, r, x, acked)
				if acked {
					r.wantOnlyTheStatusReport(t, x)
					return
				}
				r.wantUnreportedRetransmitting(t, scKey(x), c.reply)
			})
		}
	}
}

// TestInitiatedHandshakeFailureUnreportedWhileSigma1IsUnacknowledged: a
// handshake this node opened gets a Sigma2 that does not decode and does
// not acknowledge the Sigma1. The message is refused on receive — not
// acknowledged, never reaching the handshake — so no StatusReport is sent,
// the handshake goes on waiting, and the Sigma1 goes on being
// retransmitted until it is acknowledged (review II, F-db021da0-7, owner
// decision 173; phase 7, decision 277: matter.js throws "Previous message
// ack is missing", packages/protocol/src/protocol/MessageExchange.ts:521-525
// at db021da0). The acknowledged case is
// TestInitiatedHandshakeKeepsItsFailureReportTracked.
func TestInitiatedHandshakeFailureUnreportedWhileSigma1IsUnacknowledged(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity:      newCaseTestIdentity(t, 0xBBBB, 1, newCaseTestIPK(t)),
		Verifier:      caseTestVerifier{},
		SessionID:     0x1004,
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
	// A Sigma2 that is none, and that carries no acknowledgement.
	hdr := message.Header{MessageCounter: 78, DestSize: message.DestNodeID, DestNodeID: sigma1Hdr.SourceNodeID}
	proto := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeSigma2, ExchangeID: sigma1.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck: true,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	datagram = append(datagram, 0xFF)
	if err := r.b.dispatch(ctx, datagram, r.peer.LocalAddr()); !errors.Is(err, errPreviousAckMissing) {
		t.Fatalf("dispatch(Sigma2): %v, want errPreviousAckMissing", err)
	}
	select {
	case err := <-done:
		t.Fatalf("the handshake ended (%v) on a Sigma2 refused on receive", err)
	case <-time.After(200 * time.Millisecond):
	}
	r.wantUnreportedRetransmitting(t, HandshakeKey{InitiatorNodeID: sigma1Hdr.SourceNodeID, ExchangeID: sigma1.ExchangeID}, mrp.SCOpcodeSigma1)
}

// TestUnreportedRetransmissionEndsWithStop: the reply an unreported failure
// leaves retransmitting is retransmitted by the ACK pump, so it stops with
// Stop — it does not outlive the bridge (owner decision 173).
func TestUnreportedRetransmissionEndsWithStop(t *testing.T) {
	t.Parallel()
	// The ACK pump runs only when its tracker is attached before Start.
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	r := &unsecuredRig{b: b, peer: peer}
	const x = 0x5200
	paseFailsAtPake3(t, r, x, false)
	// The Pake2 the failure left goes out again.
	if _, proto := r.nextMessage(t, "the Pake2 before the failure"); proto.Opcode != mrp.SCOpcodePake2 {
		t.Fatalf("first datagram %#x, want Pake2", proto.Opcode)
	}
	if _, proto := r.nextMessage(t, "the Pake2 retransmitted after the failure"); proto.Opcode != mrp.SCOpcodePake2 {
		t.Fatalf("retransmission %#x, want Pake2", proto.Opcode)
	}
	if err := r.b.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	buf := make([]byte, 1500)
	_ = r.peer.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		n, from, err := r.peer.ReadFromUDP(buf)
		if err != nil {
			return // nothing more after Stop
		}
		if !unsecuredFromBridge(r.b, from, buf[:n]) {
			continue
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			continue
		}
		if proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n]); err == nil && proto.Opcode == mrp.SCOpcodePake2 {
			t.Fatal("the Pake2 was retransmitted after Stop")
		}
	}
}
