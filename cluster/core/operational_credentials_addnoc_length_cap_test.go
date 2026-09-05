// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestPin_AddNOC_LengthCap pins that AddNOC rejects NOC and ICAC payloads
// larger than 400 bytes — chip src/credentials/CHIPCert.h
// kMaxCHIPCertLength, enforced in
// operational-credentials-server.cpp before the TLV decoder ever sees the
// input. An oversized certificate is malformed by construction; accepting
// it hands unbounded attacker-controlled input to the decoder during
// commissioning, when the peer is not yet authenticated.
//
// The cap is measured behaviourally rather than by scanning the source for
// the literal: each case drives the real MatterInvoke(AddNOC) path and
// reads the in-band NOCResponse the spec mandates (Matter §11.18.6.7 —
// AddNOC reports failure via NOCResponse.StatusCode, not via the IM error
// channel).
//
// The 400-byte case is the negative control that ties the assertion to the
// boundary itself: a payload exactly at the cap must pass the length gate
// and fail later, for a different reason. Without it a broken cap (say,
// "reject everything") would still satisfy the over-cap cases.
func TestPin_AddNOC_LengthCap(t *testing.T) {
	t.Parallel()

	const (
		atCap   = 400
		overCap = 401
	)

	cases := []struct {
		name string
		noc  []byte
		icac []byte
		// wantStatus + wantDebugText describe the exact rejection the
		// length gate produces. NOCStatusMissingCsr is what the request
		// reaches when it clears the gate: no CSRRequest was issued, so
		// the very next guard rejects it.
		wantStatus    uint8
		wantDebugText string
	}{
		{
			name:          "NOC one byte over the cap",
			noc:           make([]byte, overCap),
			wantStatus:    core.NOCStatusInvalidNOC,
			wantDebugText: "NOC exceeds 400-byte limit",
		},
		{
			name:          "ICAC one byte over the cap",
			noc:           make([]byte, atCap),
			icac:          make([]byte, overCap),
			wantStatus:    core.NOCStatusInvalidNOC,
			wantDebugText: "ICAC exceeds 400-byte limit",
		},
		{
			name:          "NOC exactly at the cap clears the length gate",
			noc:           make([]byte, atCap),
			icac:          make([]byte, atCap),
			wantStatus:    core.NOCStatusMissingCsr,
			wantDebugText: "CSR not issued",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{
				SupportedFabrics: 5,
			})
			if err != nil {
				t.Fatalf("NewOperationalCredentials: %v", err)
			}
			// AddNOC outside an armed FailSafe window is refused before
			// the length gate is reached (Matter §11.18.6.8), so the
			// window must be armed for this test to measure the cap.
			oc.SetIsFailSafeArmed(func() bool { return true })

			resp, err := oc.MatterInvoke(context.Background(), 0x06, core.AddNOCRequest{
				NOCValue:         tc.noc,
				ICACValue:        tc.icac,
				IPKValue:         make([]byte, 16),
				CaseAdminSubject: 0x0001_0203_0405_0607,
				AdminVendorID:    0x1234,
			})
			if err != nil {
				t.Fatalf("AddNOC: unexpected IM error: %v", err)
			}
			nocResp, ok := resp.(core.NOCResponse)
			if !ok {
				t.Fatalf("AddNOC response type = %T, want core.NOCResponse", resp)
			}
			if nocResp.StatusCode != tc.wantStatus {
				t.Errorf("AddNOC StatusCode = %d, want %d (DebugText=%q)",
					nocResp.StatusCode, tc.wantStatus, nocResp.DebugText)
			}
			if nocResp.DebugText != tc.wantDebugText {
				t.Errorf("AddNOC DebugText = %q, want %q", nocResp.DebugText, tc.wantDebugText)
			}
		})
	}
}
