// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for the timed-required-invoke conformance path in
// receive_dispatch.go: a timed-required command outside a timed interaction
// answers NEEDS_TIMED_INTERACTION for its own path. This file lives in
// package bridge so it can construct a bare Bridge directly, matching the
// style of receive_test.go's checkTimedGate unit tests.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// encodeTimedTestInvokeRequest encodes a minimal InvokeRequestMessage
// carrying one CommandDataIB per path, with no CommandFields — the
// dispatcher's checkTimedGate call runs from the path alone, before
// route resolution ever needs fields (see dispatchInvokeRequest in
// receive_dispatch.go). Tag numbers 0/1/2 mirror
// tagInvokeReq{SuppressResponse,TimedRequest,InvokeRequests} in
// im/invoke.go; CommandPathIB is encoded via
// [im.ConcreteCommandPath.MarshalTLV]. Mirrors the shape of
// encodeScenarioInvokeMoveToLevel in scenario_tlv_test.go.
func encodeTimedTestInvokeRequest(t *testing.T, timedRequest bool, paths ...im.ConcreteCommandPath) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), timedRequest)
	enc.StartArray(tlv.ContextTag(2)) // InvokeRequests
	for _, p := range paths {
		enc.StartStruct(tlv.AnonymousTag()) // CommandDataIB
		p.MarshalTLV(enc, tlv.ContextTag(0))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encodeTimedTestInvokeRequest: %v", err)
	}
	return body
}

// decodeStatusResponseCode extracts the Status field (tag 0) from a
// StatusResponseMessage body. Mirrors the tag layout in
// im/timed.go (tagStatusResponseStatus = 0).
func decodeStatusResponseCode(t *testing.T, body []byte) im.StatusCode {
	t.Helper()
	dec := tlv.NewDecoder(body)
	open, err := dec.Next()
	if err != nil || !open.IsContainer {
		t.Fatalf("decode StatusResponse: open struct: %v", err)
	}
	for {
		el, err := dec.Next()
		if err != nil {
			t.Fatalf("decode StatusResponse: %v", err)
		}
		if el.IsEndContainer {
			t.Fatal("decode StatusResponse: no Status (tag 0) field found")
		}
		if el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == 0 {
			return im.StatusCode(el.Uint)
		}
	}
}

// TestDispatchInvokeRequest_TimedRequiredWithoutWindow_NeedsTimedInteraction
// drives a real Bridge.dispatch with an OpenCommissioningWindow InvokeRequest
// whose own TimedRequest flag is clear and with no preceding TimedRequest,
// and asserts the reply is an InvokeResponse whose entry for that path
// carries NEEDS_TIMED_INTERACTION (0xC6) — matter.js answers it per path
// (CommandInvokeResponse.ts:291 `limits.timed && !this.session.timed`), not
// with an interaction-level StatusResponse, which matter.js reserves for a
// Timed flag that disagrees with the exchange (TIMED_REQUEST_MISMATCH).
// Uses SessionID=0 (unsecured), matching TestDispatch_IMReadRoutes's
// precedent, so no CASE session pair is needed.
func TestDispatchInvokeRequest_TimedRequiredWithoutWindow_NeedsTimedInteraction(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)

	peerConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = peerConn.Close() })
	peerAddr, ok := peerConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected peer addr type %T", peerConn.LocalAddr())
	}

	hdr := buildHeader(0, 20)
	proto := buildProtocolHeader(im.InteractionModelProtocolID, im.OpcodeInvokeRequest)
	payload := encodeTimedTestInvokeRequest(t, false, im.ConcreteCommandPath{
		Endpoint: 1, Cluster: 0x003C, Command: 0x0, // AdministratorCommissioning.OpenCommissioningWindow
		HasEndpoint: true, HasCluster: true, HasCommand: true,
	})
	buf := buildDatagram(hdr, proto, payload)

	if err := b.dispatch(context.Background(), buf, peerAddr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	rbuf := make([]byte, 1500)
	n, _, err := peerConn.ReadFromUDP(rbuf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	got := rbuf[:n]

	_, hdrLen, err := message.UnmarshalHeader(got)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	rproto, protoLen, err := message.UnmarshalProtocolHeader(got[hdrLen:])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if rproto.Opcode != im.OpcodeInvokeResponse {
		t.Fatalf("reply opcode = 0x%02X, want InvokeResponse (0x%02X)", rproto.Opcode, im.OpcodeInvokeResponse)
	}
	if status, found := firstStatusIBCode(t, got[hdrLen+protoLen:]); !found || status != im.StatusNeedsTimedInteraction {
		t.Errorf("path status = %v (found %v), want StatusNeedsTimedInteraction (0xC6)", status, found)
	}
}

// firstStatusIBCode returns the Status of the first StatusIB in an
// InvokeResponseMessage: InvokeResponses (tag 1) → InvokeResponseIB →
// CommandStatusIB (tag 1) → StatusIB (tag 1) → Status (tag 0).
func firstStatusIBCode(t *testing.T, body []byte) (im.StatusCode, bool) {
	t.Helper()
	dec := tlv.NewDecoder(body)
	var stack []uint64
	for {
		el, err := dec.Next()
		if err != nil {
			return 0, false
		}
		if el.IsEndContainer {
			if len(stack) == 0 {
				return 0, false
			}
			stack = stack[:len(stack)-1]
			continue
		}
		tag := uint64(0xFFFF)
		if el.Tag.Kind == tlv.TagKindContext {
			tag = uint64(el.Tag.Number)
		}
		if el.IsContainer {
			stack = append(stack, tag)
			continue
		}
		// [anon, 1(array), anon(InvokeResponseIB), 1(CommandStatusIB), 1(StatusIB)] → tag 0
		if tag == 0 && len(stack) == 5 && stack[1] == 1 && stack[3] == 1 && stack[4] == 1 {
			return im.StatusCode(el.Uint), true
		}
	}
}
