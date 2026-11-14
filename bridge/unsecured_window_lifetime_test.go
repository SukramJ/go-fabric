// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// sendAtCounter is pipelinePeer.send with an explicit message counter, so a
// retransmission carries the counter of the message it repeats.
func sendAtCounter(p *pipelinePeer, counter uint32, opcode uint8, exchangeID uint16, payload []byte, ack *uint32) error {
	hdr := message.Header{MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: p.nodeID}
	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: opcode,
		ExchangeID: exchangeID, ProtocolID: mrp.SecureChannelProtocolID,
	}
	if ack != nil {
		proto.HasAck, proto.AckCounter = true, *ack
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	datagram = append(datagram, payload...)
	return p.b.dispatch(context.Background(), datagram, p.conn.LocalAddr())
}

// TestUnsecuredWindowLivesWithItsHandshake — the duplicate-detection window
// of an unsecured source lives as long as the handshake it serves, as
// matter.js ties the reception state to the unsecured session that ends with
// its last exchange (packages/protocol/src/protocol/ExchangeManager.ts:244-256
// at db021da0: an unsecured session created for an inbound message does not
// outlive it unless a protocol handler adopts it).
//
// 256 datagrams from as many claimed source node ids, none of which starts a
// handshake, leave no window behind; a commissioner then completes PASE, its
// success report is lost, and the retransmitted Pake3 (same counter) is a
// duplicate: acknowledged, not answered with a failure, not counted towards
// the lockout.
func TestUnsecuredWindowLivesWithItsHandshake(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")
	pase := newPaseAdapterWithVerifier(t)
	pase.SetPBKDFParams(iterations, salt, 1)
	r.b.AttachPaseHandler(pase)

	for i := range maxUnsecuredWindows {
		s := newPipelinePeer(t, r.b, 0x1_0000_0000+uint64(i))
		_ = s.send(mrp.StandaloneAckOpcode, uint16(0x4000+i), nil, nil)
	}
	strangers := r.b.unsecuredWindows.len()

	commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
	const paseExchange = uint16(0x0101)
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	pbkdfReq := buildTestPBKDFParamRequest(t, initRand)
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, paseExchange, pbkdfReq, nil); err != nil {
		t.Fatal(err)
	}
	pbkdfResp, counter, _ := commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "PBKDFParamResponse")
	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(pbkdfReq)
	h.Write(pbkdfResp)
	prover, err := spake2.NewProver(passcode, salt, iterations, nil, nil, h.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	if err := commissioner.send(mrp.SCOpcodePake1, paseExchange, spake2.EncodePake1(pA), &counter); err != nil {
		t.Fatal(err)
	}
	pake2, counter, _ := commissioner.recv(mrp.SCOpcodePake2, "Pake2")
	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		t.Fatal(err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		t.Fatal(err)
	}
	const pake3Counter = 88888
	if err := sendAtCounter(commissioner, pake3Counter, mrp.SCOpcodePake3, paseExchange, spake2.EncodePake3(cA), &counter); err != nil {
		t.Fatalf("Pake3: %v", err)
	}
	status, _, _ := commissioner.recv(mrp.SCOpcodeStatusReport, "StatusReport")
	if general, _, code, _ := decodeStatusReport(status); general != 0 || code != 0 {
		t.Fatalf("Pake3 answered general=%d code=%d, want success", general, code)
	}

	// The success report is lost: the commissioner retransmits Pake3.
	if err := sendAtCounter(commissioner, pake3Counter, mrp.SCOpcodePake3, paseExchange, spake2.EncodePake3(cA), &counter); err != nil {
		t.Fatalf("retransmitted Pake3 reached the handshake handler: %v", err)
	}
	if n := r.b.paseFailures.Load(); n != 0 {
		t.Fatalf("retransmitted Pake3 counted %d PASE failures, want 0", n)
	}
	if n := pendingOnUnsecuredExchange(r.b, paseExchange); n != 1 {
		t.Fatalf("%d entries pending on the PASE exchange after the retransmission, want 1 (the success report)", n)
	}
	if strangers != 0 {
		t.Fatalf("%d unsecured windows after datagrams that started no handshake, want 0", strangers)
	}
}

// TestUnsecuredWindowEndsWithItsHandshake — once the handshake's last reply
// is acknowledged and nothing of the handshake is held, the next source's
// first message drops the window: nothing of a finished handshake stays in
// the table.
func TestUnsecuredWindowEndsWithItsHandshake(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	r.b.AttachPaseHandler(newPaseAdapterWithVerifier(t))
	peer := newPipelinePeer(t, r.b, 0xC0FFEE)
	const exchange = uint16(0x0202)
	// A Pake1 without a PBKDFParamRequest before it: the handshake answers
	// with a failure report, which waits for its acknowledgement — the
	// window lives with it.
	_ = peer.send(mrp.SCOpcodePake1, exchange, []byte{0x15, 0x18}, nil)
	if n := r.b.unsecuredWindows.len(); n != 1 {
		t.Fatalf("%d unsecured windows while the failure report waits for its acknowledgement, want 1", n)
	}
	_, reportCounter, _ := peer.recv(mrp.SCOpcodeStatusReport, "StatusReport")
	if err := peer.send(mrp.StandaloneAckOpcode, exchange, nil, &reportCounter); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if n := r.b.unsecuredWindows.len(); n != 0 {
		t.Fatalf("%d unsecured windows after the handshake's last message was acknowledged, want 0", n)
	}
}
