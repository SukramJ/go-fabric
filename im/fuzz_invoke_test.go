// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// FuzzUnmarshalInvokeRequest exercises the Invoke decoder against
// random byte slices. The decoder MUST NOT panic on any input —
// truncated payloads, malformed containers, oversized integers, etc.
// all need to surface as a returned error. Originated from the
// Welle-7 nil-fieldsReader-panic bug (ADR 0013 §2).
func FuzzUnmarshalInvokeRequest(f *testing.F) {
	// Seeds: a few hand-crafted shapes plus the empty case.
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})                                                                   // empty anonymous struct
	f.Add([]byte{0x15, 0x28, 0x00, 0x18})                                                       // SuppressResponse: false
	f.Add([]byte{0x15, 0x36, 0x02, 0x15, 0x37, 0x00, 0x24, 0x01, 0x05, 0x18, 0x18, 0x18, 0x18}) // path-only invoke
	f.Fuzz(func(t *testing.T, payload []byte) {
		dec := tlv.NewDecoder(payload)
		_, _ = im.UnmarshalInvokeRequestTLV(dec, nil)
		// Any panic here would FAIL via the testing harness.
	})
}

// FuzzUnmarshalReadRequest mirrors FuzzUnmarshalInvokeRequest for
// the Read path.
func FuzzUnmarshalReadRequest(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Add([]byte{0x15, 0x36, 0x00, 0x18, 0x28, 0x03, 0x18}) // empty AttributeRequests + FabricFiltered=false
	// Review 2026-11 W2-wire-03: endpoint 65536 as uint32, endpoint -1 as int8.
	f.Add([]byte{0x15, 0x36, 0x00, 0x17, 0x26, 0x02, 0x00, 0x00, 0x01, 0x00, 0x24, 0x03, 0x1D, 0x24, 0x04, 0x03, 0x18, 0x18, 0x28, 0x03, 0x18})
	f.Add([]byte{0x15, 0x36, 0x00, 0x17, 0x20, 0x02, 0xFF, 0x24, 0x03, 0x1D, 0x24, 0x04, 0x03, 0x18, 0x18, 0x28, 0x03, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		dec := tlv.NewDecoder(payload)
		_, _ = im.UnmarshalReadRequestTLV(dec)
	})
}

// FuzzUnmarshalWriteRequest mirrors FuzzUnmarshalInvokeRequest for
// the Write path.
func FuzzUnmarshalWriteRequest(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		dec := tlv.NewDecoder(payload)
		_, _ = im.UnmarshalWriteRequestTLV(dec, nil)
	})
}

// FuzzUnmarshalSubscribeRequest mirrors FuzzUnmarshalInvokeRequest
// for the Subscribe path.
func FuzzUnmarshalSubscribeRequest(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		dec := tlv.NewDecoder(payload)
		_, _ = im.UnmarshalSubscribeRequestTLV(dec)
	})
}

// FuzzTimedRequest feeds arbitrary bytes to the TimedRequest decoder, which
// arms the timed-interaction window of the follow-up Write or Invoke
// (W6-test-06): no panic, and an accepted request re-encodes (MarshalTLV, a
// client's form) to bytes that decode to the same timeout.
func FuzzTimedRequest(f *testing.F) {
	seed := tlv.NewEncoder()
	im.TimedRequest{TimeoutMs: 1000}.MarshalTLV(seed)
	b, _ := seed.Bytes()
	f.Add(b)
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Add([]byte{0x15, 0x25, 0x00, 0xFF, 0xFF, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		req, err := im.UnmarshalTimedRequestTLV(tlv.NewDecoder(payload))
		if err != nil {
			return
		}
		enc := tlv.NewEncoder()
		req.MarshalTLV(enc)
		out, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		again, err := im.UnmarshalTimedRequestTLV(tlv.NewDecoder(out))
		if err != nil || again != req {
			t.Fatalf("TimedRequest round trip: %+v -> %+v (%v)", req, again, err)
		}
	})
}

// FuzzStatusResponse is FuzzTimedRequest for the StatusResponse a peer
// sends between report chunks and after a timed request.
func FuzzStatusResponse(f *testing.F) {
	seed := tlv.NewEncoder()
	im.StatusResponse{Status: im.StatusSuccess}.MarshalTLV(seed)
	b, _ := seed.Bytes()
	f.Add(b)
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x24, 0x00, 0xC6, 0x18})
	f.Fuzz(func(t *testing.T, payload []byte) {
		sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(payload))
		if err != nil {
			return
		}
		enc := tlv.NewEncoder()
		sr.MarshalTLV(enc)
		out, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		again, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(out))
		if err != nil || again != sr {
			t.Fatalf("StatusResponse round trip: %+v -> %+v (%v)", sr, again, err)
		}
	})
}
