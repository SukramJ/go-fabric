// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestRequestsWithoutInteractionModelRevision ports matter.js
// packages/node/test/node/InteractionModelRevisionAbsentTest.ts: a read,
// write, invoke, subscribe and timed request that omit the
// InteractionModelRevision field (tag 0xFF) are served, not rejected.
func TestRequestsWithoutInteractionModelRevision(t *testing.T) {
	t.Parallel()
	attrPath := func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartList(tag)
		enc.PutUint(tlv.ContextTag(2), 0)    // endpoint
		enc.PutUint(tlv.ContextTag(3), 0x28) // BasicInformation
		enc.PutUint(tlv.ContextTag(4), 5)    // NodeLabel
		_ = enc.EndContainer()
	}
	build := func(body func(*tlv.Encoder)) *tlv.Decoder {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		body(enc)
		_ = enc.EndContainer()
		data, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return tlv.NewDecoder(data)
	}

	t.Run("read", func(t *testing.T) {
		t.Parallel()
		dec := build(func(enc *tlv.Encoder) {
			enc.StartArray(tlv.ContextTag(0))
			attrPath(enc, tlv.AnonymousTag())
			_ = enc.EndContainer()
			enc.PutBool(tlv.ContextTag(3), true) // FabricFiltered
		})
		req, err := UnmarshalReadRequestTLV(dec)
		if err != nil || len(req.AttributeRequests) != 1 {
			t.Fatalf("read without IMRevision: %v %+v", err, req)
		}
	})
	t.Run("subscribe", func(t *testing.T) {
		t.Parallel()
		dec := build(func(enc *tlv.Encoder) {
			enc.PutBool(tlv.ContextTag(0), true) // KeepSubscriptions
			enc.PutUint(tlv.ContextTag(1), 0)    // MinIntervalFloor
			enc.PutUint(tlv.ContextTag(2), 10)   // MaxIntervalCeiling
			enc.StartArray(tlv.ContextTag(3))
			attrPath(enc, tlv.AnonymousTag())
			_ = enc.EndContainer()
			enc.PutBool(tlv.ContextTag(7), true) // FabricFiltered
		})
		req, err := UnmarshalSubscribeRequestTLV(dec)
		if err != nil || len(req.AttributeRequests) != 1 {
			t.Fatalf("subscribe without IMRevision: %v %+v", err, req)
		}
	})
	t.Run("timed", func(t *testing.T) {
		t.Parallel()
		dec := build(func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 1000) })
		req, err := UnmarshalTimedRequestTLV(dec)
		if err != nil || req.TimeoutMs != 1000 {
			t.Fatalf("timed without IMRevision: %v %+v", err, req)
		}
	})
	t.Run("invoke", func(t *testing.T) {
		t.Parallel()
		dec := build(func(enc *tlv.Encoder) {
			enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
			enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
			enc.StartArray(tlv.ContextTag(2))
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartList(tlv.ContextTag(0))
			enc.PutUint(tlv.ContextTag(0), 2)    // endpoint
			enc.PutUint(tlv.ContextTag(1), 0x06) // OnOff
			enc.PutUint(tlv.ContextTag(2), 0x02) // Toggle
			_ = enc.EndContainer()
			enc.StartStruct(tlv.ContextTag(1))
			_ = enc.EndContainer()
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		})
		req, err := UnmarshalInvokeRequestTLV(dec, nil)
		if err != nil || len(req.Invokes) != 1 {
			t.Fatalf("invoke without IMRevision: %v %+v", err, req)
		}
	})
	t.Run("write", func(t *testing.T) {
		t.Parallel()
		dec := build(func(enc *tlv.Encoder) {
			enc.PutBool(tlv.ContextTag(0), false)
			enc.PutBool(tlv.ContextTag(1), false)
			enc.StartArray(tlv.ContextTag(2))
			enc.StartStruct(tlv.AnonymousTag())
			attrPath(enc, tlv.ContextTag(1))
			enc.PutUTF8(tlv.ContextTag(2), "label")
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		})
		req, err := UnmarshalWriteRequestTLV(dec, nil)
		if err != nil || len(req.Writes) != 1 {
			t.Fatalf("write without IMRevision: %v %+v", err, req)
		}
	})
}
