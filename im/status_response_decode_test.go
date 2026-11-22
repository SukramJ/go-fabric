// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestUnmarshalStatusResponseTLV_RoundTrip pins that the decoder reads
// back what MarshalTLV writes, for a Success and for the two statuses a
// controller uses to disown a subscription (ServerSubscription.ts:866).
func TestUnmarshalStatusResponseTLV_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, status := range []StatusCode{StatusSuccess, StatusFailure, StatusInvalidSubscription} {
		enc := tlv.NewEncoder()
		StatusResponse{Status: status}.MarshalTLV(enc)
		wire, err := enc.Bytes()
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		got, err := UnmarshalStatusResponseTLV(tlv.NewDecoder(wire))
		if err != nil {
			t.Fatalf("status %v: UnmarshalStatusResponseTLV: %v", status, err)
		}
		if got.Status != status {
			t.Errorf("decoded status %v, want %v", got.Status, status)
		}
	}
}

// TestUnmarshalStatusResponseTLV_Malformed pins the rejects: a missing
// Status field, a non-structure top level, and a status wider than the
// uint8 the schema allows.
func TestUnmarshalStatusResponseTLV_Malformed(t *testing.T) {
	t.Parallel()
	noStatus := func() []byte {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(tagStatusResponseInteractionModelRevision), uint64(InteractionModelRevision))
		_ = enc.EndContainer()
		b, _ := enc.Bytes()
		return b
	}()
	wideStatus := func() []byte {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(tagStatusResponseStatus), 0x100)
		_ = enc.EndContainer()
		b, _ := enc.Bytes()
		return b
	}()
	notStruct := func() []byte {
		enc := tlv.NewEncoder()
		enc.PutUint(tlv.AnonymousTag(), 0)
		b, _ := enc.Bytes()
		return b
	}()
	for name, wire := range map[string][]byte{"no status": noStatus, "wide status": wideStatus, "not a struct": notStruct, "empty": nil} {
		_, err := UnmarshalStatusResponseTLV(tlv.NewDecoder(wire))
		if !errors.Is(err, ErrInvalidStatusResponse) {
			t.Errorf("%s: err = %v, want ErrInvalidStatusResponse", name, err)
		}
	}
}

// TestUnmarshalStatusResponseTLV_WrongType (review II, R2-P6-wire-0503):
// a Status that is not an unsigned integer is refused, as matter.js's
// TlvEnum (a TlvUInt32) throws on another TLV type
// (packages/types/src/tlv/TlvNumber.ts:289, :124-131 at db021da0) — it used to
// decode as Success.
func TestUnmarshalStatusResponseTLV_WrongType(t *testing.T) {
	t.Parallel()
	for name, put := range map[string]func(*tlv.Encoder){
		"signed":    func(e *tlv.Encoder) { e.PutInt(tlv.ContextTag(tagStatusResponseStatus), 1) },
		"bool":      func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(tagStatusResponseStatus), true) },
		"null":      func(e *tlv.Encoder) { e.PutNull(tlv.ContextTag(tagStatusResponseStatus)) },
		"structure": func(e *tlv.Encoder) { e.StartStruct(tlv.ContextTag(tagStatusResponseStatus)); _ = e.EndContainer() },
	} {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		put(enc)
		_ = enc.EndContainer()
		wire, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if sr, err := UnmarshalStatusResponseTLV(tlv.NewDecoder(wire)); !errors.Is(err, ErrInvalidStatusResponse) {
			t.Errorf("%s: decoded %+v, err %v; want ErrInvalidStatusResponse", name, sr, err)
		}
	}
}
