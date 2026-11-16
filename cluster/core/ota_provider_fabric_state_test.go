// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// TestOTAProviderDropFabricForgetsItsUpdates (review II, R2-P6-bound-1206,
// owner decision 167): the provider is fabric-scoped state — it names the
// fabrics its in-progress entries and consents belong to, and a removed
// fabric's entries and consents go (their cleanup run); another fabric's
// stay.
func TestOTAProviderDropFabricForgetsItsUpdates(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query %+v", r)
	}
	f.apply(t, []byte{1, 2, 3, 4}, 2)
	other := OTAPeer{FabricIndex: otaTestFabric + 1, NodeID: 5}
	f.p.mu.Lock()
	f.p.consents[other] = 9
	f.p.mu.Unlock()
	if got := f.p.MatterHeldFabrics(); !slices.Equal(got, []uint8{otaTestFabric, otaTestFabric + 1}) {
		t.Fatalf("held %v", got)
	}
	if err := f.p.MatterDropFabric(context.Background(), otaTestFabric); err != nil {
		t.Fatal(err)
	}
	f.p.mu.Lock()
	n, kept := len(f.p.inProgress), f.p.consents[other]
	_, gone := f.p.consents[otaTestPeer]
	f.p.mu.Unlock()
	if n != 0 || gone || kept != 9 {
		t.Errorf("after the drop: %d entries, own consent kept %v, other's consent %d", n, gone, kept)
	}
	if got := f.p.MatterHeldFabrics(); !slices.Equal(got, []uint8{otaTestFabric + 1}) {
		t.Errorf("held after the drop %v", got)
	}
}
