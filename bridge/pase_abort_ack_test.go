// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestPASEAbortReportIsAcknowledged: a commissioner that ends a PASE
// handshake with a reliable failure StatusReport gets that report
// acknowledged, by a standalone acknowledgement to the address it came
// from: the handshake's end drops the replies it still owes, not the
// acknowledgement it owes for the message that ended it.
func TestPASEAbortReportIsAcknowledged(t *testing.T) {
	t.Parallel()
	r := newUnsecuredRig(t)
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
		return a
	})
	r.b.AttachPaseHandlerProvider(provider.Resolve)

	commissioner := newPipelinePeer(t, r.b, 0xC0FFEE)
	const paseExchange = uint16(0x0101)
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, paseExchange, buildTestPBKDFParamRequest(t, initRand), nil); err != nil {
		t.Fatal(err)
	}
	_, counter, _ := commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "PBKDFParamResponse")

	abort := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	if err := commissioner.send(mrp.SCOpcodeStatusReport, paseExchange, abort, &counter); err != nil {
		t.Fatal(err)
	}
	reportCounter := commissioner.counter
	// The rig attaches its AckTracker after Start, so no pump runs: one
	// pass past the tracker's grace window stands in for it.
	r.b.RunAckPumpOnce(r.b.clk().Now().Add(time.Second))

	buf := make([]byte, 1500)
	for {
		_ = commissioner.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := commissioner.conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("the commissioner's failure StatusReport (counter %d) was not acknowledged: %v", reportCounter, err)
		}
		if !unsecuredFromBridge(r.b, from, buf[:n]) {
			continue
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatalf("header: %v", err)
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil {
			t.Fatalf("protocol header: %v", err)
		}
		if proto.HasAck && proto.AckCounter == reportCounter {
			if proto.Opcode != mrp.StandaloneAckOpcode || proto.ExchangeID != paseExchange {
				t.Fatalf("the report was acknowledged by opcode %#x on exchange %#x, want a standalone acknowledgement on %#x", proto.Opcode, proto.ExchangeID, paseExchange)
			}
			return
		}
	}
}

// TestSecureStatusReportDoesNotAbortPASE: a failure StatusReport on a
// secure session names no handshake, so it does not end the PASE handshake
// in flight even when its exchange id and source node id are the
// handshake's (R2-P6-test-0821); the same report on the unsecured session
// does.
func TestSecureStatusReportDoesNotAbortPASE(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	key := HandshakeKey{InitiatorNodeID: 0x5A5C, ExchangeID: 0x0102}
	if !b.claimPaseInFlight(key) {
		t.Fatal("the PASE claim was refused")
	}
	inFlight := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return !b.paseInFlightSince.IsZero() && b.paseInFlight == key
	}
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	proto := message.ProtocolHeader{ProtocolID: mrp.SecureChannelProtocolID, Opcode: mrp.SCOpcodeStatusReport, ExchangeID: key.ExchangeID}
	secure := &message.Header{SessionID: 0x0901, MessageCounter: 7, HasSourceNodeID: true, SourceNodeID: key.InitiatorNodeID}
	if b.abortPaseOnStatusReport(loopbackSrc(), secure, proto, failure) {
		t.Error("a secure session's report was taken as the PASE handshake's")
	}
	if !inFlight() {
		t.Fatal("a secure session's failure report ended the PASE handshake in flight")
	}
	unsecured := &message.Header{SessionID: 0, MessageCounter: 8, HasSourceNodeID: true, SourceNodeID: key.InitiatorNodeID}
	if !b.abortPaseOnStatusReport(loopbackSrc(), unsecured, proto, failure) || inFlight() {
		t.Fatal("the unsecured failure report did not end the PASE handshake")
	}
}
