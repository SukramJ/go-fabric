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
		n, _, err := commissioner.conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("the commissioner's failure StatusReport (counter %d) was not acknowledged: %v", reportCounter, err)
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
