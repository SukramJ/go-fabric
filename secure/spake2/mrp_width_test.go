// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestMRPParameters_RetransmitTimeoutsAreUint32OnTheWire — a commissioner
// may advertise an idle interval above 65535 ms (a sleepy device's
// commissioner, or simply a long one). The two timeouts are TlvUInt32 in
// matter.js PaseMessages.ts:25-29 and uint32 on the CASE path of this
// module; decoding them into 16 bits wrapped 100000 to 34464 and paced
// retransmissions to that peer three times too fast.
func TestMRPParameters_RetransmitTimeoutsAreUint32OnTheWire(t *testing.T) {
	t.Parallel()
	idle, active, thresh := uint32(100000), uint32(70000), uint16(4000)
	resp := spake2.PBKDFParamResponse{
		InitiatorRandom:    bytes.Repeat([]byte{0x11}, spake2.PBKDFRandomSize),
		ResponderRandom:    bytes.Repeat([]byte{0x22}, spake2.PBKDFRandomSize),
		ResponderSessionID: 9,
		ResponderMRPParams: &spake2.MRPParameters{
			IdleRetransTimeoutMs:   &idle,
			ActiveRetransTimeoutMs: &active,
			ActiveThresholdTimeMs:  &thresh,
		},
	}
	got, err := spake2.DecodePBKDFParamResponse(resp.Marshal())
	if err != nil {
		t.Fatalf("DecodePBKDFParamResponse: %v", err)
	}
	pm := got.ResponderMRPParams
	if pm == nil || pm.IdleRetransTimeoutMs == nil || pm.ActiveRetransTimeoutMs == nil {
		t.Fatalf("MRP params did not round-trip: %+v", pm)
	}
	if *pm.IdleRetransTimeoutMs != idle {
		t.Errorf("idle = %d, want %d", *pm.IdleRetransTimeoutMs, idle)
	}
	if *pm.ActiveRetransTimeoutMs != active {
		t.Errorf("active = %d, want %d", *pm.ActiveRetransTimeoutMs, active)
	}
}
