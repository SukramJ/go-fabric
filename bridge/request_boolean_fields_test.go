// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// booleanFieldPayload encodes a minimal Read, Subscribe, Write or Invoke
// request whose boolean fields are booleans, except the one named bad,
// which carries an unsigned integer; bad "" is the well-formed request.
// Tags: TlvInvokeRequest.ts:19-25, TlvWriteRequest.ts:18-25,
// TlvReadRequest.ts:21-30, TlvSubscribeRequest.ts:21-39,
// TlvAttributePath.ts:21-23, TlvEventPath.ts:19, :22 at db021da0.
func booleanFieldPayload(t *testing.T, opcode uint8, bad string) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	b := func(tag uint8, name string) {
		if name == bad {
			enc.PutUint(tlv.ContextTag(tag), 1)
			return
		}
		enc.PutBool(tlv.ContextTag(tag), false)
	}
	end := func() {
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
	}
	paths := func(attrTag, eventTag uint8) {
		enc.StartArray(tlv.ContextTag(attrTag))
		enc.StartList(tlv.AnonymousTag())
		b(0, "EnableTagCompression")
		enc.PutUint(tlv.ContextTag(2), 0) // endpoint
		enc.PutUint(tlv.ContextTag(3), 0x0028)
		enc.PutUint(tlv.ContextTag(4), 0) // DataModelRevision
		end()
		end()
		enc.StartArray(tlv.ContextTag(eventTag))
		enc.StartList(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), 0) // endpoint
		b(4, "IsUrgent")
		end()
		end()
	}
	enc.StartStruct(tlv.AnonymousTag())
	switch opcode {
	case im.OpcodeInvokeRequest:
		b(0, "SuppressResponse")
		b(1, "TimedRequest")
		enc.StartArray(tlv.ContextTag(2))
		end()
	case im.OpcodeWriteRequest:
		b(0, "SuppressResponse")
		b(1, "TimedRequest")
		enc.StartArray(tlv.ContextTag(2))
		end()
		b(3, "MoreChunkedMessages")
	case im.OpcodeReadRequest:
		paths(0, 1)
		b(3, "FabricFiltered")
	case im.OpcodeSubscribeRequest:
		b(0, "KeepSubscriptions")
		enc.PutUint(tlv.ContextTag(1), 0)
		enc.PutUint(tlv.ContextTag(2), 60)
		paths(3, 4)
		b(7, "FabricFiltered")
	default:
		t.Fatalf("opcode 0x%02X", opcode)
	}
	end()
	data, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRequestBooleanFieldOfAnotherTypeAnsweredFailure: through the receive
// path, a Read, Subscribe, Write or Invoke whose boolean field carries
// another TLV type is answered with a StatusResponse FAILURE and not
// processed — a TimedRequest of another type no longer runs the request as
// untimed. Mirrors matter.js at db021da0: TlvBoolean.ts:22-24 throws
// UnexpectedDataError, InteractionMessenger.handleRequest
// (packages/protocol/src/interaction/InteractionMessenger.ts:344) decodes
// each request inside its try (:360, :377, :389, :395) and answers an error that carries no status with
// Status.Failure (:425-460), as SuppressResponseTest.ts:395 asserts for a
// statusless error. The well-formed request is the control: it passes the
// decode and is answered with its own response or its handler's status.
func TestRequestBooleanFieldOfAnotherTypeAnsweredFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		opcode, response uint8
		fields           []string
	}{
		{"invoke", im.OpcodeInvokeRequest, im.OpcodeInvokeResponse, []string{"SuppressResponse", "TimedRequest"}},
		{"write", im.OpcodeWriteRequest, im.OpcodeWriteResponse, []string{"SuppressResponse", "TimedRequest", "MoreChunkedMessages"}},
		{"read", im.OpcodeReadRequest, im.OpcodeReportData, []string{"FabricFiltered", "EnableTagCompression", "IsUrgent"}},
		{"subscribe", im.OpcodeSubscribeRequest, im.OpcodeReportData, []string{"KeepSubscriptions", "FabricFiltered", "EnableTagCompression", "IsUrgent"}},
	}
	for _, c := range cases {
		t.Run("control "+c.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reply := dispatchReply(t, b, c.opcode, booleanFieldPayload(t, c.opcode, ""), 1)
			if reply == nil {
				t.Fatal("no reply")
			}
			_, hdrLen, err := message.UnmarshalHeader(reply)
			if err != nil {
				t.Fatal(err)
			}
			proto, protoLen, err := message.UnmarshalProtocolHeader(reply[hdrLen:])
			if err != nil {
				t.Fatal(err)
			}
			if proto.Opcode == im.OpcodeStatusResponse {
				// The handler's own refusal (a Subscribe that matches
				// nothing here: InvalidAction) is past the decode.
				if got := decodeStatusResponseCode(t, reply[hdrLen+protoLen:]); got == im.StatusFailure {
					t.Fatal("control answered FAILURE")
				}
				return
			}
			if proto.Opcode != c.response {
				t.Fatalf("reply opcode 0x%02X, want 0x%02X", proto.Opcode, c.response)
			}
		})
		for _, field := range c.fields {
			t.Run(c.name+" "+field, func(t *testing.T) {
				t.Parallel()
				b := newStartedBridge(t)
				reply := dispatchReply(t, b, c.opcode, booleanFieldPayload(t, c.opcode, field), 1)
				if reply == nil {
					t.Fatal("no reply")
				}
				if got := decodeWriteChunkedStatusResponse(t, reply); got != im.StatusFailure {
					t.Fatalf("status %v, want FAILURE", got)
				}
			})
		}
	}
}
