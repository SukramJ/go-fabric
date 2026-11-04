// SPDX-License-Identifier: Apache-2.0
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
	buf := secureTestDatagram(t, b, buildProtocolHeader(im.InteractionModelProtocolID, opcode), payload)
	_ = b.dispatch(context.Background(), buf, peer.LocalAddr().(*net.UDPAddr))
	_ = peer.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	rbuf := make([]byte, 1500)
	n, _, err := peer.ReadFromUDP(rbuf)
	if err != nil {
		return nil
	}
	return openTestReplyFor(t, b, rbuf[:n])
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
			// SuppressResponseTest.ts:384-397 answers the unexpected error
			// of an unsuppressed request with FAILURE; its Go form is the
			// request the bridge cannot decode.
			name: "undecodable invoke", opcode: im.OpcodeInvokeRequest,
			payload: malformedInvoke,
			status:  im.StatusFailure,
		},
		{
			// SuppressResponseTest.ts:230-240: one path more than
			// MaxPathsPerInvoke is InvalidAction.
			name: "too many paths", opcode: im.OpcodeInvokeRequest,
			payload: func(s bool) []byte {
				paths := make([]im.ConcreteCommandPath, im.DefaultMaxPathsPerInvoke+1)
				for i := range paths {
					paths[i] = im.ConcreteCommandPath{Endpoint: uint16(i), Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}
				}
				return encodeInvokeWith(t, s, false, paths...)
			},
			status: im.StatusInvalidAction,
		},
		{
			// SuppressResponseTest.ts:278-282: SuppressResponse and nothing
			// else (ONLY_SUPPRESS_RESPONSE, :62-64).
			name: "invoke of SuppressResponse alone", opcode: im.OpcodeInvokeRequest,
			payload: onlySuppressResponse,
			status:  suppressedOnly,
		},
		{
			// SuppressResponseTest.ts:284-288, the same for a write.
			name: "write of SuppressResponse alone", opcode: im.OpcodeWriteRequest,
			payload: onlySuppressResponse,
			status:  suppressedOnly,
		},
	}
	counter := uint32(100)
	for _, c := range cases {
		counter++
		if c.status == suppressedOnly {
			counter++
			if reply := dispatchReply(t, b, c.opcode, c.payload(true), counter); reply != nil {
				t.Errorf("%s with SuppressResponse: the bridge sent a %d-byte reply, want none", c.name, len(reply))
			}
			continue
		}
		reply := dispatchReply(t, b, c.opcode, c.payload(false), counter)
		if reply == nil {
			t.Errorf("%s without SuppressResponse: no StatusResponse", c.name)
		} else if got := decodeWriteChunkedStatusResponse(t, reply); got != c.status && c.status != anyStatus {
			t.Errorf("%s without SuppressResponse: status %v, want %v", c.name, got, c.status)
		}
		counter++
		if reply := dispatchReply(t, b, c.opcode, c.payload(true), counter); reply != nil {
			t.Errorf("%s with SuppressResponse: the bridge sent a %d-byte reply, want none", c.name, len(reply))
		}
	}
}

// anyStatus marks a case whose unsuppressed answer is a StatusResponse of
// whatever status the decoder's refusal carries; suppressedOnly a case
// matter.js states only with SuppressResponse set.
const (
	anyStatus      im.StatusCode = 0xFF
	suppressedOnly im.StatusCode = 0xFE
)

// onlySuppressResponse is a request of the SuppressResponse field alone —
// ONLY_SUPPRESS_RESPONSE of SuppressResponseTest.ts:62-64 when suppress.
func onlySuppressResponse(suppress bool) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), suppress)
	_ = enc.EndContainer()
	body, _ := enc.Bytes()
	return body
}

// unreadableSuppressResponse is UNREADABLE_SUPPRESS_RESPONSE
// (SuppressResponseTest.ts:66-68): tag 0 an unsigned integer.
func unreadableSuppressResponse() []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 1)
	_ = enc.EndContainer()
	body, _ := enc.Bytes()
	return body
}

// TestSuppressResponseErrorPathsParityMatterJS ports the remaining cases of
// matter.js packages/node/test/endpoint/server/SuppressResponseTest.ts at
// 862ad75f that need more than one datagram's worth of setup: a seeded
// timed window, a chunked write, and the requests whose SuppressResponse
// cannot be read.
func TestSuppressResponseErrorPathsParityMatterJS(t *testing.T) {
	t.Parallel()
	nodeLabel := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	onOff := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}

	// SuppressResponseTest.ts:220-228: the TimedRequest window expired
	// before the suppressed invoke — TIMEOUT, not sent. The window is
	// seeded expired on the test session's exchange (buildProtocolHeader
	// uses exchange 1), as the gate tests do.
	t.Run("sends nothing for a suppressed invoke after the timed window expired", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		b.routing.timedDeadlines.Store(timedKey{sessionID: paseTestSessionID, exchangeID: 1}, time.Now().Add(-time.Millisecond))
		if reply := dispatchReply(t, b, im.OpcodeInvokeRequest, encodeInvokeWith(t, true, true, onOff), 1); reply != nil {
			t.Fatalf("the bridge sent a %d-byte reply, want none", len(reply))
		}
		if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: paseTestSessionID, exchangeID: 1}); ok {
			t.Fatal("the expired window was not consumed")
		}
	})

	// SuppressResponseTest.ts:251-260: MoreChunkedMessages with
	// SuppressResponse is InvalidAction, not sent.
	t.Run("sends nothing for a suppressed write rejected with InvalidAction", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		if reply := dispatchReply(t, b, im.OpcodeWriteRequest, encodeWriteChunkedTestRequest(t, true, false, true, nodeLabel), 1); reply != nil {
			t.Fatalf("the bridge sent a %d-byte reply, want none", len(reply))
		}
	})

	// SuppressResponseTest.ts:329-335: a write whose SuppressResponse is
	// absent is answered — TimedRequestMismatch for its timed flag.
	t.Run("answers a write without SuppressResponse", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutBool(tlv.ContextTag(1), true) // TimedRequest, SuppressResponse absent
		enc.StartArray(tlv.ContextTag(2))
		enc.StartStruct(tlv.AnonymousTag())
		nodeLabel.MarshalTLV(enc, tlv.ContextTag(1))
		enc.PutUTF8(tlv.ContextTag(2), "written")
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		body, _ := enc.Bytes()
		reply := dispatchReply(t, b, im.OpcodeWriteRequest, body, 1)
		if reply == nil {
			t.Fatal("no StatusResponse")
		}
		if got := decodeWriteChunkedStatusResponse(t, reply); got != im.StatusTimedRequestMismatch {
			t.Fatalf("status %v, want TIMED_REQUEST_MISMATCH", got)
		}
	})

	// SuppressResponseTest.ts:337-343 and :399-403: a SuppressResponse that
	// cannot be read does not suppress — the request is answered with a
	// StatusResponse.
	for _, c := range []struct {
		name   string
		opcode uint8
	}{
		{"answers an invoke whose SuppressResponse cannot be read", im.OpcodeInvokeRequest},
		{"answers a write whose SuppressResponse cannot be read", im.OpcodeWriteRequest},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reply := dispatchReply(t, b, c.opcode, unreadableSuppressResponse(), 1)
			if reply == nil {
				t.Fatal("no reply")
			}
			decodeWriteChunkedStatusResponse(t, reply) // fails unless a StatusResponse
		})
	}
}
