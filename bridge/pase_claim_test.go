// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

func newPaseClaimRig(t *testing.T) (*unsecuredRig, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	r := newUnsecuredRigOn(t, clk)
	provider := NewPerExchangePaseProvider(func() *PaseAdapter {
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
		return a
	})
	provider.SetClock(clk)
	r.b.AttachPaseHandlerProvider(provider.Resolve)
	return r, clk
}

func freshPBKDFParamRequest(t *testing.T) []byte {
	t.Helper()
	initRand := make([]byte, spake2.PBKDFRandomSize)
	if _, err := rand.Read(initRand); err != nil {
		t.Fatal(err)
	}
	return buildTestPBKDFParamRequest(t, initRand)
}

// none asserts that the bridge sends the peer nothing but acknowledgements
// within d.
func (p *pipelinePeer) none(d time.Duration, what string) {
	p.t.Helper()
	buf := make([]byte, 1500)
	deadline := time.Now().Add(d)
	for {
		_ = p.conn.SetReadDeadline(deadline)
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		_, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			continue
		}
		proto, _, err := message.UnmarshalProtocolHeader(buf[hl:n])
		if err != nil || proto.Opcode == mrp.StandaloneAckOpcode {
			continue
		}
		p.t.Fatalf("%s: the bridge answered with opcode %#x", what, proto.Opcode)
	}
}

// TestPaseClaimRunsFromItsFirstRequest: the single-active-PASE claim runs
// pasePairingTimeout from the PBKDFParamRequest that took it; a further
// PBKDFParamRequest of the same handshake is acknowledged and not answered
// and renews nothing; and the claim that ran out counts one PASE failure
// when another commissioner takes the slot (review II, R2-P6-sec-0304).
func TestPaseClaimRunsFromItsFirstRequest(t *testing.T) {
	r, clk := newPaseClaimRig(t)
	holder := newPipelinePeer(t, r.b, 0xBAD)
	const xid = uint16(0x0101)
	if err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	holder.recv(mrp.SCOpcodePBKDFParamResponse, "the first PBKDFParamRequest")
	clk.Advance(pasePairingTimeout - time.Second)
	if err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	holder.none(300*time.Millisecond, "a repeated PBKDFParamRequest of the claim's handshake")
	clk.Advance(2 * time.Second)
	other := HandshakeKey{InitiatorNodeID: 0x600D, ExchangeID: 0x0202}
	if r.b.paseHandshakeOfAnother(other) {
		t.Fatal("the claim still holds the slot past pasePairingTimeout from its first request")
	}
	if got := r.b.paseFailures.Load(); got != 0 {
		t.Fatalf("PASE failures before the slot was taken over: %d", got)
	}
	commissioner := newPipelinePeer(t, r.b, other.InitiatorNodeID)
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, other.ExchangeID, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "another commissioner after the claim ran out")
	if got := r.b.paseFailures.Load(); got != 1 {
		t.Fatalf("PASE failures after the abandoned claim was taken over: %d, want 1", got)
	}
}

// TestTwentyAbandonedPaseClaimsLockPASEOut: each claim abandoned until it ran
// out counts as one failed pairing, so twenty of them engage the lockout
// that twenty failed passcode attempts engage (review II, R2-P6-sec-0304).
func TestTwentyAbandonedPaseClaimsLockPASEOut(t *testing.T) {
	r, clk := newPaseClaimRig(t)
	for i := range paseMaxErrors + 1 {
		if i == paseMaxErrors && r.b.paseLockedOut() {
			t.Fatalf("PASE locked out after %d abandoned claims, before the %dth", i-1, paseMaxErrors)
		}
		p := newPipelinePeer(t, r.b, uint64(0x7000+i))
		if err := p.send(mrp.SCOpcodePBKDFParamRequest, uint16(0x0300+i), freshPBKDFParamRequest(t), nil); err != nil {
			t.Fatal(err)
		}
		clk.Advance(pasePairingTimeout + time.Second)
	}
	if !r.b.paseLockedOut() {
		t.Fatal("twenty abandoned claims did not lock PASE out")
	}
}

// TestFailureReportOfAnotherInitiatorKeepsTheHold: an unsecured failure
// report releases a connection's handshake hold only when it is of the
// hold's own handshake — its initiator and exchange — so a report sent
// under another source node id on the same exchange id ends nothing
// (review II, R2-P6-sec-0304, N3-fixrrdev-2).
func TestFailureReportOfAnotherInitiatorKeepsTheHold(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	route := b.routeFor(&fakeConn{})
	r, ok := route.(*connRoute)
	if !ok {
		t.Fatalf("route %T", route)
	}
	key := HandshakeKey{InitiatorNodeID: 0x1111, ExchangeID: 0x40}
	b.holdHandshake(route, key, true)
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	hdr := &message.Header{MessageCounter: 5, HasSourceNodeID: true, SourceNodeID: 0x2222}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodeStatusReport, ExchangeID: key.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID}
	_ = b.dispatchSecureChannel(route, hdr, proto, failure)
	b.routeBindMu.Lock()
	_, held := r.handshakes[key.ExchangeID]
	b.routeBindMu.Unlock()
	if !held {
		t.Fatal("a failure report of another initiator released the hold")
	}
	b.settleHandshake(route, HandshakeKey{InitiatorNodeID: 0x2222, ExchangeID: key.ExchangeID}, nil, mrp.SCOpcodeStatusReport, failure, nil)
	b.routeBindMu.Lock()
	_, held = r.handshakes[key.ExchangeID]
	b.routeBindMu.Unlock()
	if !held {
		t.Fatal("a step of another initiator's handshake released the hold")
	}
	hdr.SourceNodeID = key.InitiatorNodeID
	_ = b.dispatchSecureChannel(route, hdr, proto, failure)
	b.routeBindMu.Lock()
	_, held = r.handshakes[key.ExchangeID]
	b.routeBindMu.Unlock()
	if held {
		t.Fatal("the hold's own failure report did not release it")
	}
}
