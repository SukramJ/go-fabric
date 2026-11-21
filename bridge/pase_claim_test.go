// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"crypto/rand"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/mdns"
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
		n, from, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !unsecuredFromBridge(p.b, from, buf[:n]) {
			continue
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
// pasePairingTimeout from the PBKDFParamRequest that took it, and the claim
// that ran out counts one PASE failure when another commissioner takes the
// slot (review II, R2-P6-sec-0304).
func TestPaseClaimRunsFromItsFirstRequest(t *testing.T) {
	r, clk := newPaseClaimRig(t)
	holder := newPipelinePeer(t, r.b, 0xBAD)
	const xid = uint16(0x0101)
	if err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	holder.recv(mrp.SCOpcodePBKDFParamResponse, "the first PBKDFParamRequest")
	clk.Advance(pasePairingTimeout + time.Second)
	other := HandshakeKey{InitiatorNodeID: 0x600D, ExchangeID: 0x0202}
	if r.b.paseHandshakeOfAnother(other) {
		t.Fatal("the claim still holds the slot past pasePairingTimeout from its first request")
	}
	if got := r.b.failedAttempts.Load(); got != 0 {
		t.Fatalf("PASE failures before the slot was taken over: %d", got)
	}
	commissioner := newPipelinePeer(t, r.b, other.InitiatorNodeID)
	if err := commissioner.send(mrp.SCOpcodePBKDFParamRequest, other.ExchangeID, freshPBKDFParamRequest(t), nil); err != nil {
		t.Fatal(err)
	}
	commissioner.recv(mrp.SCOpcodePBKDFParamResponse, "another commissioner after the claim ran out")
	if got := r.b.failedAttempts.Load(); got != 1 {
		t.Fatalf("PASE failures after the abandoned claim was taken over: %d, want 1", got)
	}
}

// TestRepeatedPBKDFParamRequestEndsThePairing: a further PBKDFParamRequest
// on the handshake that holds the claim is an error of that pairing, as
// matter.js's PASE server takes an unexpected message on the accepted
// exchange: it counts one PASE failure, and the pairing ends — the slot is
// free for the next commissioner at once, and a Pake1 of the ended
// handshake begins nothing. It is answered with a StatusReport
// InvalidParameter (review II, R2-P6-sec-0304, rounds 2 and 4). While the
// PBKDFParamResponse still waits for its acknowledgement, a repeated
// request that does not acknowledge it is refused on receive — not
// acknowledged, not processed — and the pairing goes on: matter.js throws
// "Previous message ack is missing" before the message reaches the PASE
// server (packages/protocol/src/protocol/MessageExchange.ts:521-525 at
// db021da0; review II, phase 7, decision 277).
func TestRepeatedPBKDFParamRequestEndsThePairing(t *testing.T) {
	for _, acked := range []bool{true, false} {
		t.Run(map[bool]string{true: "the response acknowledged", false: "the response unacknowledged"}[acked], func(t *testing.T) {
			r, _ := newPaseClaimRig(t)
			holder := newPipelinePeer(t, r.b, 0xBAD)
			const xid = uint16(0x0101)
			if err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), nil); err != nil {
				t.Fatal(err)
			}
			_, counter, _ := holder.recv(mrp.SCOpcodePBKDFParamResponse, "the first PBKDFParamRequest")
			var ack *uint32
			if acked {
				ack = &counter
			}
			err := holder.send(mrp.SCOpcodePBKDFParamRequest, xid, freshPBKDFParamRequest(t), ack)
			if !acked {
				if !errors.Is(err, errPreviousAckMissing) {
					t.Fatalf("a repeated PBKDFParamRequest that does not acknowledge the response: dispatch %v, want errPreviousAckMissing", err)
				}
				holder.none(300*time.Millisecond, "a repeated PBKDFParamRequest while the response waits for its acknowledgement")
				if got := r.b.failedAttempts.Load(); got != 0 {
					t.Fatalf("PASE failures after a refused PBKDFParamRequest: %d, want 0", got)
				}
				if !r.b.paseHandshakeOfAnother(HandshakeKey{InitiatorNodeID: 0x600D, ExchangeID: 0x0202}) {
					t.Fatal("the refused message freed the pairing's slot")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			report, _, _ := holder.recv(mrp.SCOpcodeStatusReport, "the answer to a repeated PBKDFParamRequest")
			if general, _, code, ok := decodeStatusReport(report); !ok || general != mrp.SCStatusGeneralFailure || code != mrp.SCStatusProtocolInvalidParameter {
				t.Fatalf("report general %#x code %#x, want failure / InvalidParameter", general, code)
			}
			if got := r.b.failedAttempts.Load(); got != 1 {
				t.Fatalf("PASE failures after a repeated PBKDFParamRequest: %d, want 1", got)
			}
			other := HandshakeKey{InitiatorNodeID: 0x600D, ExchangeID: 0x0202}
			if r.b.paseHandshakeOfAnother(other) {
				t.Fatal("the ended pairing still holds the slot")
			}
			if _, begun := r.b.resolvePaseContinuation(HandshakeKey{InitiatorNodeID: 0xBAD, ExchangeID: xid}); begun {
				t.Fatal("a Pake1 of the ended pairing would still reach its handler")
			}
		})
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

// TestPaseClaimIsReadUnderTheLock: a claim racing the release of the slot
// reads the slot's holder under the bridge's lock — run under -race
// (review II, R2-P6-sec-0304, round 2). Each claimer released its own
// claim before it claims again, so a claim finds the slot free or the
// other's, never its own; once both are done the slot is free.
func TestPaseClaimIsReadUnderTheLock(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	a, other := HandshakeKey{InitiatorNodeID: 1, ExchangeID: 1}, HandshakeKey{InitiatorNodeID: 2, ExchangeID: 2}
	claimAndRelease := func(key HandshakeKey) {
		if got := b.claimPase(key); got == paseClaimRepeated {
			t.Errorf("claim %v found its own released claim in the slot", key)
		}
		runtime.Gosched()
		b.releasePaseInFlight(key)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20000 {
			claimAndRelease(a)
		}
	}()
	for range 20000 {
		claimAndRelease(other)
	}
	<-done
	if got := b.claimPase(a); got != paseClaimTaken {
		t.Fatalf("claim after both released = %v, want the slot free (taken)", got)
	}
}

// TestPASEClaimHeldUntilItsTimeoutPasses: the single PASE slot is held at
// exactly pasePairingTimeout after the claim — another handshake is busy,
// the claiming one still continues — and free only past it
// (R2-P6-bound-0813).
func TestPASEClaimHeldUntilItsTimeoutPasses(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := HandshakeKey{InitiatorNodeID: 1, ExchangeID: 1}
	second := HandshakeKey{InitiatorNodeID: 2, ExchangeID: 2}
	if b.claimPase(first) != paseClaimTaken {
		t.Fatal("the first claim was not taken")
	}
	clk.Advance(pasePairingTimeout)
	if got := b.claimPase(second); got != paseClaimBusy {
		t.Fatalf("a second handshake at exactly the timeout: claim %v, want busy", got)
	}
	if _, begun := b.resolvePaseContinuation(first); !begun {
		t.Fatal("the claiming handshake no longer continues at exactly the timeout")
	}
	clk.Advance(time.Nanosecond)
	if got := b.claimPase(second); got != paseClaimTaken {
		t.Fatalf("a second handshake past the timeout: claim %v, want taken", got)
	}
}
