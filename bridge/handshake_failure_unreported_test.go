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

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// wantNoReport asserts that the handshake on exchangeID holds no entry and
// that no StatusReport reached the peer. A report is sent synchronously
// with the failing message's handling, so it is in the peer's socket
// already when it was sent at all.
func (r *unsecuredRig) wantNoReport(t *testing.T, exchangeID uint16) {
	t.Helper()
	if got := r.tracked(t, exchangeID); len(got) != 0 {
		t.Errorf("entries of the exchange after its handshake ended unreported: opcodes %#x, want none", got)
	}
	buf := make([]byte, 1500)
	for {
		_ = r.peer.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, from, err := r.peer.ReadFromUDP(buf)
		if err != nil {
			return // nothing more on the wire
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
// and what it sent is dropped; once that reply is acknowledged, the same
// failure is reported, reliably (review II, F-db021da0-7; the register row
// in notes/parity/matter_behaviour_findings.md). Before, the report was
// sent reliably in both cases, after the drop. One case per role and per
// way a failure leaves the handler: an error, and a failure report as the
// handler's reply.
func TestHandshakeFailureUnreportedWhileAReplyIsUnacknowledged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(t *testing.T, r *unsecuredRig, x uint16, acked bool)
	}{
		{"PASE, the handler refuses Pake1", paseFailsAtPake1},
		{"CASE, the handler refuses Sigma3", caseFailsAtSigma3},
		{"CASE, the shipped adapter answers Sigma3 with its refusal", caseRefusedAtSigma3},
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
				r.wantNoReport(t, x)
			})
		}
	}
}

// TestInitiatedHandshakeFailureUnreportedWhileSigma1IsUnacknowledged: a
// handshake this node opened gets a Sigma2 that does not decode and does
// not acknowledge the Sigma1. It ends without a StatusReport, and the
// Sigma1 is dropped (review II, F-db021da0-7). The acknowledged case is
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
	r.wantNoReport(t, sigma1.ExchangeID)
}
