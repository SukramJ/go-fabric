// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// FuzzDecodeStatusReport feeds DecodeStatusReport the Secure Channel
// StatusReport bodies a peer sends. Invariant: it never panics, and ok
// holds exactly when the body has the 8-byte fixed part. A decoded report
// encodes back to the bytes it came from, so no field is lost or invented.
func FuzzDecodeStatusReport(f *testing.F) {
	f.Add(exchange.StatusReport{GeneralCode: 1, ProtocolID: 0, ProtocolCode: 2}.Encode())
	f.Add(exchange.StatusReport{GeneralCode: 0, ProtocolID: 0xFFF1_0001, ProtocolCode: 0, ProtocolData: []byte{1, 2, 3}}.Encode())
	f.Add([]byte{})
	f.Add(make([]byte, 7))
	f.Fuzz(func(t *testing.T, payload []byte) {
		r, ok := exchange.DecodeStatusReport(payload)
		if ok != (len(payload) >= 8) {
			t.Fatalf("DecodeStatusReport(%d bytes) ok = %v", len(payload), ok)
		}
		if !ok {
			return
		}
		if got := r.Encode(); !bytes.Equal(got, payload) {
			t.Fatalf("re-encoded %x, want %x", got, payload)
		}
	})
}
