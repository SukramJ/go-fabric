// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
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
