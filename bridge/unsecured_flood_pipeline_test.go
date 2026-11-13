// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// oneFabricResolver is the destination-id check of a node that holds one
// fabric: a Sigma1 addressed to any other is refused before a key is
// generated (secure/sigma Responder.ProcessSigma1, ErrNoSharedTrustRoots).
type oneFabricResolver struct {
	ours    *sigma.Identity
	rootPub []byte
}

func (r oneFabricResolver) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	if sigma.ComputeDestinationID(r.ours.IPK, initiatorRandom, r.rootPub, r.ours.FabricID, r.ours.NodeID) != destinationID {
		return nil, nil, false
	}
	return r.ours, caseTestVerifier{}, true
}

// pipelinePeer is a node on the link that speaks to the bridge on the
// unsecured session: every message it sends enters [Bridge.dispatch] as a
// datagram, every reply is read from its own UDP socket.
type pipelinePeer struct {
	t       *testing.T
	b       *Bridge
	conn    *net.UDPConn
	nodeID  uint64
	counter uint32
}

func newPipelinePeer(t *testing.T, b *Bridge, nodeID uint64) *pipelinePeer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &pipelinePeer{t: t, b: b, conn: conn, nodeID: nodeID, counter: 1000}
}

// send delivers one handshake message through the receive pipeline; ack,
// when set, piggybacks the acknowledgement of the bridge's last message.
func (p *pipelinePeer) send(opcode uint8, exchangeID uint16, payload []byte, ack *uint32) error {
	p.counter++
	hdr := message.Header{MessageCounter: p.counter, HasSourceNodeID: true, SourceNodeID: p.nodeID}
	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: opcode != mrp.StandaloneAckOpcode, Opcode: opcode,
		ExchangeID: exchangeID, ProtocolID: mrp.SecureChannelProtocolID,
	}
	if ack != nil {
		proto.HasAck, proto.AckCounter = true, *ack
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	datagram = append(datagram, payload...)
	return p.b.dispatch(context.Background(), datagram, p.conn.LocalAddr())
}

// recv reads the bridge's next message with opcode want and returns its
// payload, its message counter and whether it asks for an acknowledgement.
// Standalone acknowledgements on the way are skipped.
func (p *pipelinePeer) recv(want uint8, what string) (payload []byte, counter uint32, needsAck bool) {
	p.t.Helper()
	buf := make([]byte, 1500)
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			p.t.Fatalf("%s: nothing reached the peer: %v", what, err)
		}
		hdr, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			p.t.Fatalf("%s: header: %v", what, err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			p.t.Fatalf("%s: protocol header: %v", what, err)
		}
		if proto.Opcode == mrp.StandaloneAckOpcode {
			continue
		}
		if proto.Opcode != want {
			p.t.Fatalf("%s: opcode %#x reached the peer, want %#x", what, proto.Opcode, want)
		}
		return append([]byte(nil), buf[hl+protocolHeaderSize(proto):n]...), hdr.MessageCounter, proto.NeedsAck
	}
}

// pendingOnUnsecuredExchange counts the unsecured session's entries of
// exchangeID that still wait for an acknowledgement.
func pendingOnUnsecuredExchange(b *Bridge, exchangeID uint16) int {
	tr := b.outboundReliable
	tr.mu.Lock()
	defer tr.mu.Unlock()
	n := 0
	for key, e := range tr.pending {
		if key.sessionID == 0 && e.exchangeID == exchangeID {
			n++
		}
	}
	return n
}

// TestSigma1FloodDoesNotStopLegitimateHandshakes is owner decision 23 of
// review II (R2-P0-rv-A01, -A02) at the receive pipeline: a node on the
// link without any credential sends 500 well-formed Sigma1, each from a
// source node id and on an exchange of its own, each for a fabric this node
// does not hold and each stating the largest session intervals the field
// holds — and never acknowledges the StatusReport it gets. A commissioner's
// PASE and a controller's CASE handshake run through the middle of it and
// complete: every reply of theirs is sent although the unsecured session's
// retransmit table is full, because a full table costs its oldest entry
// the retransmissions, never a new reply its transmission.
//
// Nothing is lost on this link. What loss would change: a handshake reply
// whose entry the flood has evicted is not retransmitted, and the peer's
// retransmission of the request it answers is acknowledged as a duplicate
// of the unsecured reception window and not answered again
// ([Bridge.dispatch]); so a handshake whose reply is lost while a flood
// runs fails at the initiator's own timeout and has to be started again.
// Under the bound this replaces — refuse the reply that does not fit — the
// same flood stopped the handshake with no loss at all: this test failed at
// its first PBKDFParamResponse.
func TestSigma1FloodDoesNotStopLegitimateHandshakes(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)

	// The node: one fabric, a CASE responder per exchange that refuses a
	// Sigma1 for any other fabric, and a PASE responder for a passcode.
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub := elliptic.Marshal(elliptic.P256(), root.X, root.Y) //nolint:staticcheck // SA1019: test fixture
	ipk := newCaseTestIPK(t)
	ours := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	var caseSessions, paseSessions, responders atomic.Int32
	provider := NewPerExchangeCaseProvider(func() *CaseAdapter {
		responder := sigma.NewResponder(ours, caseTestVerifier{}, uint16(0x2000+responders.Add(1)))
		responder.SetIdentityResolver(oneFabricResolver{ours: ours, rootPub: rootPub})
		a := NewCaseAdapter(responder)
		a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { caseSessions.Add(1); return nil })
		return a
	})
	r.b.AttachCaseHandlerProvider(provider.Resolve)
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")
	pase := newPaseAdapterWithVerifier(t)
	pase.SetPBKDFParams(iterations, salt, 1)
	pase.SetOnSessionEstablished(func([]byte, uint16) error { paseSessions.Add(1); return nil })
	r.b.AttachPaseHandler(pase)

	// The flood: its replies go to a socket that reads and never answers.
	flooder, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = flooder.Close() })
	sent := 0
	strangerSigma1 := func() []byte {
		// An identity under an IPK this node does not have.
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
		return sigma1
	}
	floodOne := func() {
		t.Helper()
		sent++
		hdr := message.Header{MessageCounter: uint32(sent), HasSourceNodeID: true, SourceNodeID: 0xF100_0000 + uint64(sent)}
		proto := message.ProtocolHeader{
			Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma1,
			ExchangeID: uint16(0x8000 + sent), ProtocolID: mrp.SecureChannelProtocolID,
		}
		datagram := append(hdr.Marshal(), proto.Marshal()...)
		datagram = append(datagram, strangerSigma1()...)
		if err := r.b.dispatch(context.Background(), datagram, flooder.LocalAddr()); err != nil {
			t.Fatalf("Sigma1 %d of the flood: %v", sent, err)
		}
	}
	// What one Sigma1 of the flood earns, read once so the test knows what
	// it floods with: a StatusReport(NoSharedTrustRoots) that asks for an
	// acknowledgement.
	floodOne()
	{
		buf := make([]byte, 1500)
		_ = flooder.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := flooder.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("the first Sigma1 of the flood was not answered: %v", err)
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			t.Fatal(err)
		}
		_, _, code, ok := decodeStatusReport(buf[hl+protocolHeaderSize(proto) : n])
		if proto.Opcode != mrp.SCOpcodeStatusReport || !proto.NeedsAck || !ok || code != mrp.SCStatusProtocolNoSharedTrustRoots {
			t.Fatalf("the flood's Sigma1 earned opcode %#x, needs ack %v, protocol code %d — want a reliable StatusReport(NoSharedTrustRoots): the test floods with something else than it says", proto.Opcode, proto.NeedsAck, code)
		}
		_ = flooder.SetReadDeadline(time.Time{})
	}
	go func() { // the rest is read and dropped
		buf := make([]byte, 1500)
		for {
			if _, _, err := flooder.ReadFromUDP(buf); err != nil {
				return
			}
		}
	}()
	flood := func(n int) {
		t.Helper()
		for range n {
			floodOne()
		}
	}

	flood(unsecuredEntryLimit - 1)
	if got := r.pending(); got != unsecuredEntryLimit {
		t.Fatalf("%d entries pending after %d Sigma1, want the table full (%d)", got, sent, unsecuredEntryLimit)
	}

	// PASE, at a full table and with the flood between its messages.
	commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
	const paseExchange = uint16(0x0101)
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	pbkdfReq := buildTestPBKDFParamRequest(t, initRand)
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, paseExchange, pbkdfReq, nil); err != nil {
		t.Fatalf("PBKDFParamRequest at a full table: %v", err)
	}
	pbkdfResp, counter, _ := commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "PBKDFParamResponse at a full table")
	flood(160)
	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(pbkdfReq)
	h.Write(pbkdfResp)
	prover, err := spake2.NewProver(passcode, salt, iterations, nil, nil, h.Sum(nil))
	if err != nil {
		t.Fatalf("NewProver: %v", err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatalf("GeneratePake1: %v", err)
	}
	if err := commissioner.send(mrp.SCOpcodePake1, paseExchange, spake2.EncodePake1(pA), &counter); err != nil {
		t.Fatalf("Pake1: %v", err)
	}
	pake2, counter, _ := commissioner.recv(mrp.SCOpcodePake2, "Pake2")
	flood(160)
	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		t.Fatalf("DecodePake2: %v", err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		t.Fatalf("the commissioner refused Pake2: %v", err)
	}
	if err := commissioner.send(mrp.SCOpcodePake3, paseExchange, spake2.EncodePake3(cA), &counter); err != nil {
		t.Fatalf("Pake3: %v", err)
	}
	status, counter, needsAck := commissioner.recv(mrp.SCOpcodeStatusReport, "the PASE StatusReport")
	if general, _, code, ok := decodeStatusReport(status); !ok || general != mrp.SCStatusGeneralSuccess || code != mrp.SCStatusProtocolSessionEstablishmentSuccess {
		t.Fatalf("the PASE handshake ended with general %d, protocol code %d, want SessionEstablishmentSuccess", general, code)
	}
	if got := paseSessions.Load(); got != 1 {
		t.Fatalf("%d PASE sessions established, want 1", got)
	}
	// The report that ends the handshake is retransmitted until it is
	// acknowledged, and it is all the handshake still holds.
	if got := pendingOnUnsecuredExchange(r.b, paseExchange); !needsAck || got != 1 {
		t.Errorf("after the PASE StatusReport: needs ack %v, %d entries of its exchange pending — want the report alone, awaiting its acknowledgement", needsAck, got)
	}
	if err := commissioner.send(mrp.StandaloneAckOpcode, paseExchange, nil, &counter); err != nil {
		t.Fatalf("the commissioner's acknowledgement: %v", err)
	}
	if got := pendingOnUnsecuredExchange(r.b, paseExchange); got != 0 {
		t.Errorf("%d entries of the PASE exchange pending after its last acknowledgement, want 0", got)
	}

	// CASE, the same way.
	controller := newPipelinePeer(t, r.b, 0xC0DE)
	const caseExchange = uint16(0x0202)
	initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
		Identity:      newCaseTestIdentity(t, 0xAAAA, 1, ipk),
		Verifier:      caseTestVerifier{},
		SessionID:     0x1002,
		PeerNodeID:    0xBBBB,
		RootPublicKey: rootPub,
	})
	if err != nil {
		t.Fatalf("NewPeerInitiator: %v", err)
	}
	sigma1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1: %v", err)
	}
	if err := controller.send(mrp.SCOpcodeSigma1, caseExchange, sigma1, nil); err != nil {
		t.Fatalf("Sigma1 at a full table: %v", err)
	}
	sigma2, counter, _ := controller.recv(mrp.SCOpcodeSigma2, "Sigma2 at a full table")
	flood(500 - sent)
	if got := pendingOnUnsecuredExchange(r.b, caseExchange); got != 0 {
		t.Fatalf("%d entries of the CASE exchange pending after the flood, want Sigma2 evicted: the test does not exercise the eviction", got)
	}
	sigma3, err := initiator.ProcessSigma2Bytes(sigma2)
	if err != nil {
		t.Fatalf("the controller refused Sigma2: %v", err)
	}
	if err := controller.send(mrp.SCOpcodeSigma3, caseExchange, sigma3, &counter); err != nil {
		t.Fatalf("Sigma3: %v", err)
	}
	status, counter, needsAck = controller.recv(mrp.SCOpcodeStatusReport, "the CASE StatusReport")
	if general, _, code, ok := decodeStatusReport(status); !ok || general != mrp.SCStatusGeneralSuccess || code != mrp.SCStatusProtocolSessionEstablishmentSuccess {
		t.Fatalf("the CASE handshake ended with general %d, protocol code %d, want SessionEstablishmentSuccess", general, code)
	}
	if got := caseSessions.Load(); got != 1 {
		t.Fatalf("%d CASE sessions established, want 1", got)
	}
	if got := pendingOnUnsecuredExchange(r.b, caseExchange); !needsAck || got != 1 {
		t.Errorf("after the CASE StatusReport: needs ack %v, %d entries of its exchange pending — want the report alone, awaiting its acknowledgement", needsAck, got)
	}
	if err := controller.send(mrp.StandaloneAckOpcode, caseExchange, nil, &counter); err != nil {
		t.Fatalf("the controller's acknowledgement: %v", err)
	}
	if got := pendingOnUnsecuredExchange(r.b, caseExchange); got != 0 {
		t.Errorf("%d entries of the CASE exchange pending after its last acknowledgement, want 0", got)
	}

	if sent != 500 {
		t.Fatalf("the flood sent %d Sigma1, want 500", sent)
	}
	if got := r.pending(); got > unsecuredEntryLimit {
		t.Errorf("%d entries pending after the flood, want at most %d", got, unsecuredEntryLimit)
	}
}
