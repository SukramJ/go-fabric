// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// booleanFieldRequest encodes a minimal Read, Subscribe, Write or Invoke
// request whose boolean fields are booleans, except the one named bad,
// which carries an unsigned integer. bad "" is the well-formed request.
func booleanFieldRequest(t *testing.T, msg, bad string) []byte {
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
		b(tagAttrPathEnableTagCompression, "EnableTagCompression")
		enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 0)
		end()
		end()
		enc.StartArray(tlv.ContextTag(eventTag))
		enc.StartList(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(tagEventPathEndpoint), 0)
		b(tagEventPathIsUrgent, "IsUrgent")
		end()
		end()
	}
	enc.StartStruct(tlv.AnonymousTag())
	switch msg {
	case "invoke":
		b(tagInvokeReqSuppressResponse, "SuppressResponse")
		b(tagInvokeReqTimedRequest, "TimedRequest")
		enc.StartArray(tlv.ContextTag(tagInvokeReqInvokeRequests))
		end()
	case "write":
		b(tagWriteReqSuppressResponse, "SuppressResponse")
		b(tagWriteReqTimedRequest, "TimedRequest")
		enc.StartArray(tlv.ContextTag(tagWriteReqWriteRequests))
		end()
		b(tagWriteReqMoreChunked, "MoreChunkedMessages")
	case "read":
		paths(tagReadReqAttributeRequests, tagReadReqEventRequests)
		b(tagReadReqFabricFiltered, "FabricFiltered")
	case "subscribe":
		b(tagSubReqKeepSubscriptions, "KeepSubscriptions")
		enc.PutUint(tlv.ContextTag(tagSubReqMinIntervalFloor), 0)
		enc.PutUint(tlv.ContextTag(tagSubReqMaxIntervalCeiling), 60)
		paths(tagSubReqAttributeRequests, tagSubReqEventRequests)
		b(tagSubReqFabricFiltered, "FabricFiltered")
	default:
		t.Fatalf("unknown message %q", msg)
	}
	end()
	data, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// booleanFieldCases is every boolean field of the four request decoders:
// TlvReadRequest.ts:30, TlvSubscribeRequest.ts:21, :39,
// TlvInvokeRequest.ts:19, :22, TlvWriteRequest.ts:18, :21, :25,
// TlvAttributePath.ts:21 and TlvEventPath.ts:22 at db021da0, each a
// TlvBoolean.
var booleanFieldCases = []struct{ msg, field string }{
	{"invoke", "SuppressResponse"},
	{"invoke", "TimedRequest"},
	{"write", "SuppressResponse"},
	{"write", "TimedRequest"},
	{"write", "MoreChunkedMessages"},
	{"read", "FabricFiltered"},
	{"read", "EnableTagCompression"},
	{"read", "IsUrgent"},
	{"subscribe", "KeepSubscriptions"},
	{"subscribe", "FabricFiltered"},
	{"subscribe", "EnableTagCompression"},
	{"subscribe", "IsUrgent"},
}

func decodeBooleanFieldRequest(msg string, data []byte) error {
	dec := tlv.NewDecoder(data)
	var err error
	switch msg {
	case "invoke":
		_, err = UnmarshalInvokeRequestTLV(dec, nil)
	case "write":
		_, err = UnmarshalWriteRequestTLV(dec, nil)
	case "read":
		_, err = UnmarshalReadRequestTLV(dec)
	case "subscribe":
		_, err = UnmarshalSubscribeRequestTLV(dec)
	}
	return err
}

// TestRequestBooleanFieldOfAnotherTypeFailsTheDecode: a boolean request
// field that carries another TLV type fails the decode with an error that
// names no status, so the bridge answers it FAILURE, never reads it as
// false. Mirrors matter.js at db021da0: TlvBoolean.ts:22-24 throws
// UnexpectedDataError for a non-boolean element, and
// InteractionMessenger.handleRequest
// (packages/protocol/src/interaction/InteractionMessenger.ts:344) answers
// an error that carries no status with Status.Failure (:425-460), as
// SuppressResponseTest.ts:395 asserts for SuppressResponse. The
// well-formed request of each message decodes (the control).
func TestRequestBooleanFieldOfAnotherTypeFailsTheDecode(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{"invoke", "write", "read", "subscribe"} {
		if err := decodeBooleanFieldRequest(msg, booleanFieldRequest(t, msg, "")); err != nil {
			t.Fatalf("%s control: %v", msg, err)
		}
	}
	for _, c := range booleanFieldCases {
		t.Run(c.msg+"/"+c.field, func(t *testing.T) {
			t.Parallel()
			err := decodeBooleanFieldRequest(c.msg, booleanFieldRequest(t, c.msg, c.field))
			if err == nil {
				t.Fatalf("a %s of TLV type unsigned integer decoded", c.field)
			}
			var sce StatusCodeError
			if errors.As(err, &sce) {
				t.Fatalf("err = %v carries status %v, want none (answered FAILURE)", err, sce.MatterStatusCode())
			}
		})
	}
}
