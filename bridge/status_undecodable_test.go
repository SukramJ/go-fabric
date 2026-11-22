// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// undecodableStatusResponse is a StatusResponse whose Status is a signed
// integer: { 0: int8 1 }.
var undecodableStatusResponse = []byte{0x15, 0x20, 0x00, 0x01, 0x18}

// TestUndecodableAnswerToAReportIsAFailedUpdate (review II,
// R2-P6-wire-0503's bridge half, owner decision 176): a StatusResponse
// whose Status cannot be decoded, answering an ongoing report, is a failed
// send that is retried — the subscription stays, the failed update is
// counted — not the peer's refusal. matter.js (fable-6a-w3 H2):
// ServerSubscription.ts:938-944 rethrows a decode error past the
// terminating branch, :558-608 counts it and requeues.
func TestUndecodableAnswerToAReportIsAFailedUpdate(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1801)
	r.report(t)
	r.pending(t)
	var key mrp.ExchangeKey
	waitFor(t, "a report waiting for its StatusResponse", func() bool {
		found := false
		r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			if ek, ok := k.(mrp.ExchangeKey); ok && ek.SessionID == r.reportSID && ek.Initiator {
				key, found = ek, true
				return false
			}
			return true
		})
		return found
	})
	hdr := &message.Header{SessionID: key.SessionID}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse, ExchangeID: key.ExchangeID}
	if err := r.dev.br.absorbStatusResponse(nil, hdr, proto, undecodableStatusResponse); err != nil {
		t.Fatalf("absorbStatusResponse: %v", err)
	}
	waitFor(t, "the failed update", func() bool {
		raw, counted := r.dev.br.subSendErrorCount.Load(r.sub.ID)
		return counted && raw == 1
	})
	if _, err := r.dev.subs.Get(r.sub.ID); err != nil {
		t.Fatalf("subscription ended by an undecodable answer: %v", err)
	}
	r.sessionsKept(t, "after an undecodable answer")
}

// TestUndecodableAnswerToThePrimingReportFails (review II,
// R2-P6-wire-0503's bridge half, owner decision 176): a StatusResponse
// whose Status cannot be decoded, answering the priming report on the
// Subscribe's exchange, is answered StatusResponse(FAILURE) and the
// subscription is not established. matter.js (fable-6a-w3 H2):
// InteractionServer.ts:828-834 closes the subscription and rethrows;
// InteractionMessenger.ts:431, :458-463 send Failure and close the
// exchange.
func TestUndecodableAnswerToThePrimingReportFails(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)

	peerConn, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 102}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 1, Initiator: true}
	req := im.SubscribeRequest{
		AttributeRequests:  []im.ConcreteAttributePath{accessControlACLPath()},
		MaxIntervalCeiling: 60,
	}
	done := make(chan error, 1)
	go func() { done <- b.handleSubscribeRequest(context.Background(), peerAddr, hdr, proto, req) }()

	chunkHdr, chunk, err := readSubscribeReply(b, peerConn, 3*time.Second)
	if err != nil {
		t.Fatalf("no priming report: %v", err)
	}
	if chunk.Opcode != im.OpcodeReportData {
		t.Fatalf("first reply opcode = 0x%02X, want ReportData", chunk.Opcode)
	}
	answerHdr := &message.Header{SessionID: chunkHdr.SessionID, MessageCounter: 103}
	answer := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse, ExchangeID: chunk.ExchangeID, Initiator: !chunk.Initiator}
	if err := b.absorbStatusResponse(peerAddr, answerHdr, answer, undecodableStatusResponse); err != nil {
		t.Fatalf("absorbStatusResponse: %v", err)
	}
	if err := <-done; !errors.Is(err, errChunkAnswerUndecodable) {
		t.Fatalf("handleSubscribeRequest = %v, want the undecodable answer", err)
	}
	after, payload, err := readSubscribeReplyPayload(b, peerConn, time.Second)
	if err != nil {
		t.Fatalf("nothing followed the undecodable answer: %v, want StatusResponse(FAILURE)", err)
	}
	if after.Opcode != im.OpcodeStatusResponse || after.ExchangeID != chunk.ExchangeID {
		t.Fatalf("opcode 0x%02X on exchange %d followed the undecodable answer, want StatusResponse on %d", after.Opcode, after.ExchangeID, chunk.ExchangeID)
	}
	if sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(payload)); err != nil || sr.Status != im.StatusFailure {
		t.Errorf("status after the undecodable answer = %+v, %v; want FAILURE", sr, err)
	}
	if n := mgr.Active(); n != 0 {
		t.Errorf("mgr.Active() = %d after the undecodable answer, want 0", n)
	}
}

// readSubscribeReplyPayload is [readSubscribeReply] with the message's
// application payload.
func readSubscribeReplyPayload(b *Bridge, peerConn *net.UDPConn, d time.Duration) (message.ProtocolHeader, []byte, error) {
	_ = peerConn.SetReadDeadline(time.Now().Add(d))
	rbuf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(rbuf)
	if err != nil {
		return message.ProtocolHeader{}, nil, err
	}
	got := rbuf[:n]
	hdr, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		return message.ProtocolHeader{}, nil, err
	}
	b.mu.RLock()
	pase, isPASE := b.sessions.(paseTestLookup)
	b.mu.RUnlock()
	body := got[hdrLen:]
	if isPASE {
		if body, _, err = pase.peer.Decrypt(&hdr, securityFlagsByte(&hdr), body); err != nil {
			return message.ProtocolHeader{}, nil, err
		}
	}
	rproto, protoLen, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		return message.ProtocolHeader{}, nil, err
	}
	return rproto, body[protoLen:], nil
}

// TestUndecodableAnswerToAReadChunkFails (owner decision 176, the read on
// the request's own exchange as the priming report — fable-6a-w3 H2,
// InteractionMessenger.ts:431, :458-463): the read ends with
// StatusResponse(FAILURE).
func TestUndecodableAnswerToAReadChunkFails(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 93
	b := newStartedBridge(t)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	markPASE(b, sessionID)
	d := &wideCountingDispatcher{endpoints: 20, attrs: 25}
	req := im.ReadRequest{AttributeRequests: repeatedWildcardPaths(200)}
	hdr := &message.Header{SessionID: sessionID, MessageCounter: 5000}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: 0x2301, Initiator: true}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.dispatchReadRequest(ctx, peerAddr, hdr, proto, d, req) }()

	chunkHdr, chunk, _, _ := readReportChunk(t, peerConn, peerSess)
	answerHdr := &message.Header{SessionID: chunkHdr.SessionID, MessageCounter: 5001}
	answer := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse, ExchangeID: chunk.ExchangeID, Initiator: true}
	if err := b.absorbStatusResponse(peerAddr, answerHdr, answer, undecodableStatusResponse); err != nil {
		t.Fatalf("absorbStatusResponse: %v", err)
	}
	if err := <-done; !errors.Is(err, errChunkAnswerUndecodable) {
		t.Fatalf("dispatchReadRequest = %v, want the undecodable answer", err)
	}
	buf := make([]byte, 1500)
	for {
		_ = peerConn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := peerConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("nothing followed the undecodable answer: %v, want StatusResponse(FAILURE)", err)
		}
		mh, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		plain, _, err := peerSess.Decrypt(&mh, securityFlagsByte(&mh), buf[hl:n])
		if err != nil {
			t.Fatal(err)
		}
		ph, pl, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatal(err)
		}
		if ph.ProtocolID != im.InteractionModelProtocolID {
			continue // a standalone ack
		}
		if ph.Opcode != im.OpcodeStatusResponse || ph.ExchangeID != chunk.ExchangeID {
			t.Fatalf("opcode 0x%02X on exchange %d followed the undecodable answer, want StatusResponse on %d", ph.Opcode, ph.ExchangeID, chunk.ExchangeID)
		}
		if sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(plain[pl:])); err != nil || sr.Status != im.StatusFailure {
			t.Errorf("status after the undecodable answer = %+v, %v; want FAILURE", sr, err)
		}
		return
	}
}
