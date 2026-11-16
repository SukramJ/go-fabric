// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// A NOC command that finds a row an earlier rollback could not land
// settles it at its entry; the fabric-removed hook of that settle runs
// after nocMu is released, as the rule at nocMu says, so a host hook that
// calls back into the server does not block (review II, R2-P6-conc-1003).
func TestSettleReportRunsWithoutNOCLock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		fabric uint8 // the accessing fabric and the fail-safe's
		cmd    uint32
		fields any
	}{
		{"AddNOC", 0, 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1}},
		{"UpdateNOC", 1, 0x07, core.UpdateNOCRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := newFakeStore()
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(tc.fabric)})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, RootPublicKey: []byte{4, 1}}); err != nil {
				t.Fatal(err)
			}
			// A row an earlier rollback could not land: still pending.
			if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 2, RootPublicKey: []byte{4, 2}, Pending: mstore.FabricPendingAdd}); err != nil {
				t.Fatal(err)
			}
			var called, free bool
			oc.SetOnFabricRemoved(func(context.Context, uint8) {
				called = true
				free = oc.NOCLockFree()
			})
			if tc.fabric != 0 {
				ctx = im.WithFabricFilter(ctx, false, tc.fabric)
			}
			if _, err := oc.MatterInvoke(ctx, tc.cmd, tc.fields); err != nil {
				t.Logf("%s: %v", tc.name, err)
			}
			if !called {
				t.Fatal("the settle reported no removal")
			}
			if !free {
				t.Error("the fabric-removed hook of the settle ran with nocMu held")
			}
		})
	}
}
