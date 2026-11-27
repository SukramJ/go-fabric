// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestTlvWriteRequestParityMatterJS mirrors
// packages/types/test/protocol/TlvWriteRequestTest.ts:13-23 at db91886c: a
// WriteRequest that carries only WriteRequests — no SuppressResponse, no
// TimedRequest — decodes, both flags absent (false, and SuppressResponse
// not present), with an empty WriteRequests.
func TestTlvWriteRequestParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("decodes a request that leaves out SuppressResponse and TimedRequest", func(t *testing.T) {
		t.Parallel()
		// { writeRequests: [] } encoded as TlvObject with TlvField(2, ...).
		req, err := UnmarshalWriteRequestTLV(tlv.NewDecoder([]byte{0x15, 0x36, 0x02, 0x18, 0x18}), nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.SuppressResponse || req.HasSuppressResponse || req.TimedRequest || len(req.Writes) != 0 {
			t.Fatalf("request = %+v, want both flags absent and no writes", req)
		}
	})
}
