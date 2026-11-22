// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// integerFieldRequest encodes one request through enc's callback and
// returns its bytes.
func integerFieldRequest(t *testing.T, body func(enc *tlv.Encoder, end func())) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	end := func() {
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
	}
	enc.StartStruct(tlv.AnonymousTag())
	body(enc, end)
	end()
	data, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRequestIntegerFieldOfAnotherTypeAnsweredFailure: through the receive
// path, a Read, Subscribe, Write, Invoke or Timed request whose integer
// field — a path, filter or header integer — carries another TLV type, a
// signed integer included, is answered with a StatusResponse FAILURE and
// not processed; an unsigned value out of the field's range stays
// CONSTRAINT_ERROR. Mirrors matter.js at db021da0: TlvNumber.ts:124-131
// throws UnexpectedDataError for a wrong type (no StatusResponseError,
// packages/general/src/MatterError.ts:188), which
// InteractionMessenger.handleRequest answers with Status.Failure
// (packages/protocol/src/interaction/InteractionMessenger.ts:425-460);
// out of bounds is ValidationOutOfBoundsError (TlvNumber.ts:145-155), a
// ConstraintError (packages/types/src/common/ValidationError.ts:16-29).
func TestRequestIntegerFieldOfAnotherTypeAnsweredFailure(t *testing.T) {
	t.Parallel()
	attrPath := func(enc *tlv.Encoder, end, endpoint func()) {
		enc.StartList(tlv.AnonymousTag())
		endpoint()
		enc.PutUint(tlv.ContextTag(3), 0x0028)
		enc.PutUint(tlv.ContextTag(4), 0)
		end()
	}
	cases := []struct {
		name   string
		opcode uint8
		body   func(enc *tlv.Encoder, end func())
		want   im.StatusCode
	}{
		{"read AttributePathIB endpoint as int8", im.OpcodeReadRequest, func(enc *tlv.Encoder, end func()) {
			enc.StartArray(tlv.ContextTag(0))
			attrPath(enc, end, func() { enc.PutInt(tlv.ContextTag(2), 0) })
			end()
			enc.PutBool(tlv.ContextTag(3), false)
		}, im.StatusFailure},
		{"read DataVersionFilter DataVersion as a string", im.OpcodeReadRequest, func(enc *tlv.Encoder, end func()) {
			enc.StartArray(tlv.ContextTag(0))
			attrPath(enc, end, func() { enc.PutUint(tlv.ContextTag(2), 0) })
			end()
			enc.PutBool(tlv.ContextTag(3), false)
			enc.StartArray(tlv.ContextTag(4))
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartList(tlv.ContextTag(0))
			enc.PutUint(tlv.ContextTag(1), 0)
			enc.PutUint(tlv.ContextTag(2), 0x0028)
			end()
			enc.PutUTF8(tlv.ContextTag(1), "1")
			end()
			end()
		}, im.StatusFailure},
		{"read EventFilter EventMin as int8", im.OpcodeReadRequest, func(enc *tlv.Encoder, end func()) {
			enc.StartArray(tlv.ContextTag(1))
			enc.StartList(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(1), 0)
			end()
			end()
			enc.StartArray(tlv.ContextTag(2))
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutInt(tlv.ContextTag(1), 1)
			end()
			end()
			enc.PutBool(tlv.ContextTag(3), false)
		}, im.StatusFailure},
		{"subscribe MinIntervalFloor as int8", im.OpcodeSubscribeRequest, func(enc *tlv.Encoder, end func()) {
			enc.PutBool(tlv.ContextTag(0), false)
			enc.PutInt(tlv.ContextTag(1), 0)
			enc.PutUint(tlv.ContextTag(2), 60)
			enc.StartArray(tlv.ContextTag(3))
			attrPath(enc, end, func() { enc.PutUint(tlv.ContextTag(2), 0) })
			end()
			enc.PutBool(tlv.ContextTag(7), false)
		}, im.StatusFailure},
		{"invoke CommandPathIB cluster as a string", im.OpcodeInvokeRequest, func(enc *tlv.Encoder, end func()) {
			enc.PutBool(tlv.ContextTag(0), false)
			enc.PutBool(tlv.ContextTag(1), false)
			enc.StartArray(tlv.ContextTag(2))
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartList(tlv.ContextTag(0))
			enc.PutUint(tlv.ContextTag(0), 0)
			enc.PutUTF8(tlv.ContextTag(1), "A")
			enc.PutUint(tlv.ContextTag(2), 0)
			end()
			enc.StartStruct(tlv.ContextTag(1))
			end()
			end()
			end()
		}, im.StatusFailure},
		{"write AttributeDataIB DataVersion as int8", im.OpcodeWriteRequest, func(enc *tlv.Encoder, end func()) {
			enc.PutBool(tlv.ContextTag(0), false)
			enc.PutBool(tlv.ContextTag(1), false)
			enc.StartArray(tlv.ContextTag(2))
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutInt(tlv.ContextTag(0), 1)
			enc.StartList(tlv.ContextTag(1))
			enc.PutUint(tlv.ContextTag(2), 0)
			enc.PutUint(tlv.ContextTag(3), 0x0028)
			enc.PutUint(tlv.ContextTag(4), 5)
			end()
			enc.PutUTF8(tlv.ContextTag(2), "x")
			end()
			end()
		}, im.StatusFailure},
		{"timed Timeout as int16", im.OpcodeTimedRequest, func(enc *tlv.Encoder, _ func()) {
			enc.PutInt16(tlv.ContextTag(0), 1000)
		}, im.StatusFailure},
		{"subscribe MinIntervalFloor 65536 (range)", im.OpcodeSubscribeRequest, func(enc *tlv.Encoder, end func()) {
			enc.PutBool(tlv.ContextTag(0), false)
			enc.PutUint32(tlv.ContextTag(1), 0x10000)
			enc.PutUint(tlv.ContextTag(2), 60)
			enc.StartArray(tlv.ContextTag(3))
			attrPath(enc, end, func() { enc.PutUint(tlv.ContextTag(2), 0) })
			end()
			enc.PutBool(tlv.ContextTag(7), false)
		}, im.StatusConstraintError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reply := dispatchReply(t, b, c.opcode, integerFieldRequest(t, c.body), 1)
			if reply == nil {
				t.Fatal("no reply")
			}
			if got := decodeWriteChunkedStatusResponse(t, reply); got != c.want {
				t.Fatalf("status %v, want %v", got, c.want)
			}
		})
	}
}
