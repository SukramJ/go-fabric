// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
)

// TestFabricRemovalSurfacesNotifyChange holds every surface that removes a
// fabric outside the fail-safe's rollback (which TestFailSafe* cover) to the
// same rule: the change notification the bridge turns into reports of the
// cluster's reportable attributes (Fabrics, CommissionedFabrics) fires once,
// and the DataVersion moves. A host's revoke (RemoveFabric with no command
// behind it) and a surface that deleted the row itself (NotifyFabricRemoved)
// reach no invoke path that would report for them (review II, R4-par-4052).
func TestFabricRemovalSurfacesNotifyChange(t *testing.T) {
	surfaces := map[string]func(ctx context.Context, t *testing.T, oc *core.OperationalCredentials, idx uint8){
		"RemoveFabric": func(ctx context.Context, t *testing.T, oc *core.OperationalCredentials, idx uint8) {
			t.Helper()
			if err := oc.RemoveFabric(ctx, idx); err != nil {
				t.Fatal(err)
			}
		},
		"NotifyFabricRemoved": func(_ context.Context, _ *testing.T, oc *core.OperationalCredentials, idx uint8) {
			oc.NotifyFabricRemoved(idx)
		},
	}
	for sname, remove := range surfaces {
		for name, open := range restartStores {
			t.Run(sname+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				oc, err := core.NewOperationalCredentials(open(t), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
				if err != nil {
					t.Fatal(err)
				}
				_, _, idx := commissionTestFabric(ctx, t, oc)
				fired := 0
				oc.OnMatterValueChanged(func() { fired++ })
				v0 := oc.MatterDataVersion()
				remove(ctx, t, oc, idx)
				if fired != 1 {
					t.Fatalf("change notifications after the removal = %d, want 1", fired)
				}
				if oc.MatterDataVersion() == v0 {
					t.Fatal("DataVersion did not move")
				}
			})
		}
	}
}
