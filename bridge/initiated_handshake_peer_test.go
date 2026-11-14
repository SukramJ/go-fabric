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

// initiatedHandshake is a CASE handshake this node opened towards the rig's
// peer: the headers of its Sigma1, the context it runs under and the
// channel its outcome arrives on.
type initiatedHandshake struct {
	ctx      context.Context //nolint:containedctx // the handshake's own deadline, shared with the test's dispatches
	cancel   context.CancelFunc
	sigma1   message.Header
	exchange message.ProtocolHeader
	done     <-chan error
}

// startInitiatedHandshake runs a CASE handshake this node opens towards
// the rig's peer and waits for its Sigma1.
func startInitiatedHandshake(t *testing.T, r *unsecuredRig) initiatedHandshake {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
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
	peerAddr, _ := r.peer.LocalAddr().(*net.UDPAddr)
	done := make(chan error, 1)
	go func() {
		_, err := r.b.runCASEInitiator(ctx, peerAddr, &CaseInitiation{Initiator: initiator})
		done <- err
	}()
	hdr, proto := r.nextMessage(t, "the Sigma1 this node sends")
	if proto.Opcode != mrp.SCOpcodeSigma1 {
		t.Fatalf("the peer got opcode %#x, want Sigma1", proto.Opcode)
	}
	return initiatedHandshake{ctx: ctx, cancel: cancel, sigma1: hdr, exchange: proto, done: done}
}

// responderDatagram is a responder's message on the unsecured session,
// addressed to the initiator destNodeID.
func responderDatagram(counter uint32, destNodeID uint64, proto message.ProtocolHeader, payload []byte) []byte {
	hdr := message.Header{MessageCounter: counter, DestSize: message.DestNodeID, DestNodeID: destNodeID}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	return append(datagram, payload...)
}

// TestInitiatedHandshakeTakesRepliesOfItsPeerOnly: a CASE handshake this
// node opens is answered by the responder it addressed — a message on its
// exchange id that names another initiator belongs to another unsecured
// session and does not reach it. matter.js finds the unsecured session of
// a response by the destination node id it carries, "Responses include our
// ephemeral initiator nodeId as destNodeId"
// (packages/protocol/src/protocol/ExchangeManager.ts:306-308 at db021da0),
// and the exchange inside that session (:398-401); the responder side has
// the same rule ([HandshakeKey], review II R2-P3-lan-dev-01).
func TestInitiatedHandshakeTakesRepliesOfItsPeerOnly(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	h := startInitiatedHandshake(t, r)
	ctx, sigma1Hdr, sigma1, done := h.ctx, h.sigma1, h.exchange, h.done

	// A failure StatusReport on the handshake's exchange id, addressed to
	// another initiator.
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	other := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeStatusReport, ExchangeID: sigma1.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID, NeedsAck: true,
	}
	if err := r.b.dispatch(ctx, responderDatagram(500, sigma1Hdr.SourceNodeID^1, other, failure), r.peer.LocalAddr()); err != nil {
		t.Fatalf("dispatch(StatusReport of another initiator): %v", err)
	}
	// The peer the handshake addressed answers with a Sigma2 that does
	// not decode: the handshake ends on it, with its own error.
	sigma2 := message.ProtocolHeader{
		Opcode: mrp.SCOpcodeSigma2, ExchangeID: sigma1.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck: true, HasAck: true, AckCounter: sigma1Hdr.MessageCounter,
	}
	if err := r.b.dispatch(ctx, responderDatagram(77, sigma1Hdr.SourceNodeID, sigma2, []byte{0xFF}), r.peer.LocalAddr()); err != nil {
		t.Fatalf("dispatch(Sigma2): %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the handshake accepted a Sigma2 that does not decode")
		}
		if errors.Is(err, ErrCaseRejected) {
			t.Fatalf("the handshake ended on a StatusReport addressed to another initiator: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the handshake did not end")
	}
}
