// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// SuppressResponse also suppresses error statuses — matter.js
// packages/protocol/src/interaction/InteractionMessenger.ts
// InteractionServerMessenger (4bf21e80, #4570): #decodeRequest records a
// Write or Invoke request's SuppressResponse, falling back to
// suppressResponseOf(payload) when the full decode fails, and the sendStatus
// override sends nothing while it is set. Each case below fails the request
// before dispatch; with SuppressResponse the bridge sends nothing, without
// it the error StatusResponse goes out as before.

func encodeInvokeWith(t *testing.T, suppress, timed bool, paths ...im.ConcreteCommandPath) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), suppress)
	enc.PutBool(tlv.ContextTag(1), timed)
	enc.StartArray(tlv.ContextTag(2))
	for _, p := range paths {
		enc.StartStruct(tlv.AnonymousTag())
		p.MarshalTLV(enc, tlv.ContextTag(0))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// dispatchReply dispatches one Interaction Model datagram and returns the
// bridge's reply, nil when it sent none within the wait.
func dispatchReply(t *testing.T, b *Bridge, opcode uint8, payload []byte, counter uint32) []byte {
	t.Helper()
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer peer.Close()
	buf := buildDatagram(buildHeader(0, counter), buildProtocolHeader(im.InteractionModelProtocolID, opcode), payload)
	_ = b.dispatch(context.Background(), buf, peer.LocalAddr().(*net.UDPAddr))
	_ = peer.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	rbuf := make([]byte, 1500)
	n, _, err := peer.ReadFromUDP(rbuf)
	if err != nil {
		return nil
	}
	return rbuf[:n]
}

func TestParityMatterJS_SuppressResponseSuppressesErrorStatuses(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	path := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0006, Command: 0x02}
	malformedInvoke := func(suppress bool) []byte {
		// SuppressResponse readable, InvokeRequests not an array.
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutBool(tlv.ContextTag(0), suppress)
		enc.PutUint(tlv.ContextTag(2), 7)
		_ = enc.EndContainer()
		body, _ := enc.Bytes()
		return body
	}
	cases := []struct {
		name    string
		opcode  uint8
		payload func(suppress bool) []byte
		status  im.StatusCode
	}{
		{
			// Timed flag without a preceding TimedRequest:
			// TIMED_REQUEST_MISMATCH (InteractionServer.ts handleInvokeRequest).
			name: "timed mismatch", opcode: im.OpcodeInvokeRequest,
			payload: func(s bool) []byte { return encodeInvokeWith(t, s, true, path) },
			status:  im.StatusTimedRequestMismatch,
		},
		{
			// A wildcard-endpoint path in a batch: InvalidAction
			// (CommandInvokeResponse.ts #processConcrete).
			name: "batch reject", opcode: im.OpcodeInvokeRequest,
			payload: func(s bool) []byte {
				return encodeInvokeWith(t, s, false, path, im.ConcreteCommandPath{Endpoint: 2, Cluster: 0x0006, Command: 0x02})
			},
			status: im.StatusInvalidAction,
		},
		{
			name: "undecodable invoke", opcode: im.OpcodeInvokeRequest,
			payload: malformedInvoke,
			status:  im.StatusFailure,
		},
	}
	counter := uint32(100)
	for _, c := range cases {
		counter++
		reply := dispatchReply(t, b, c.opcode, c.payload(false), counter)
		if reply == nil {
			t.Errorf("%s without SuppressResponse: no StatusResponse", c.name)
		} else if got := decodeWriteChunkedStatusResponse(t, reply); got != c.status {
			t.Errorf("%s without SuppressResponse: status %v, want %v", c.name, got, c.status)
		}
		counter++
		if reply := dispatchReply(t, b, c.opcode, c.payload(true), counter); reply != nil {
			t.Errorf("%s with SuppressResponse: the bridge sent a %d-byte reply, want none", c.name, len(reply))
		}
	}
}
