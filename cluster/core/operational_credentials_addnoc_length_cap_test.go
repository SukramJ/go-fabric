// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// TestPin_AddNOC_LengthCap pins that AddNOC refuses NOC and ICAC payloads
// larger than 400 bytes with the IM status ConstraintError, before the TLV
// decoder sees them: NocValue and IcacValue carry "max 400" in the model
// (matter.js db021da0 packages/model/src/standard/elements/
// operational-credentials.element.ts:96-97), which matter.js's request
// validation enforces before addNoc runs
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:484-486).
//
// The 400-byte case is the negative control that ties the assertion to the
// boundary itself: a payload exactly at the cap passes the length gate and
// is answered in band, for a different reason (no trusted root, the first
// of addNoc's checks). Without it a broken cap (say, "reject everything")
// would still satisfy the over-cap cases.
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
		// overCap: the request is refused with ConstraintError; otherwise
		// it clears the gate and the next check answers in band.
		overCap bool
	}{
		{name: "NOC one byte over the cap", noc: make([]byte, overCap), overCap: true},
		{name: "ICAC one byte over the cap", noc: make([]byte, atCap), icac: make([]byte, overCap), overCap: true},
		{name: "NOC exactly at the cap clears the length gate", noc: make([]byte, atCap), icac: make([]byte, atCap)},
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
			oc.SetFailSafe(func() (uint8, bool) { return 0, true })

			resp, err := oc.MatterInvoke(context.Background(), 0x06, core.AddNOCRequest{
				NOCValue:         tc.noc,
				ICACValue:        tc.icac,
				IPKValue:         make([]byte, 16),
				CaseAdminSubject: 0x0001_0203_0405_0607,
				AdminVendorID:    0x1234,
			})
			if tc.overCap {
				if got := spec.StatusOf(err); got != im.StatusConstraintError {
					t.Fatalf("AddNOC over the cap: status %v (resp %+v, err %v), want ConstraintError", got, resp, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddNOC at the cap: unexpected IM error: %v", err)
			}
			nocResp, ok := resp.(core.NOCResponse)
			if !ok {
				t.Fatalf("AddNOC response type = %T, want core.NOCResponse", resp)
			}
			if nocResp.StatusCode != core.NOCStatusInvalidNOC || nocResp.DebugText != "no trusted root" {
				t.Errorf("AddNOC at the cap = %d %q, want InvalidNOC \"no trusted root\"", nocResp.StatusCode, nocResp.DebugText)
			}
		})
	}
}
