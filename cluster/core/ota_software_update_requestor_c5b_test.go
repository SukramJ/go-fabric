// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	otadef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// TestOTARequestorRefusedSaveThenDropStoresTheDrop (review II,
// R5-store-5007): a save the store refused outside a drop — here the
// removal of fabric 2's active provider — leaves the store holding what
// memory no longer does; the fabric's later drop saves although memory
// does not change, so a restart loads no provider under the index.
func TestOTARequestorRefusedSaveThenDropStoresTheDrop(t *testing.T) {
	var st *r4RefusingOtaStore
	h := newOtaHarness(t, func(c *OTARequestorConfig) {
		st = &r4RefusingOtaStore{OtaRequestorStore: c.Store}
		c.Store = st
	})
	ctx := context.Background()
	p := otadef.ProviderLocation{FabricIndex: 2, ProviderNodeId: 0x77, Endpoint: 0}
	h.r.addActiveOtaProvider(ctx, p, nil)
	st.refuse.Store(true)
	h.r.removeOtaProvider(ctx, p)
	st.refuse.Store(false)
	if err := h.r.MatterDropFabric(ctx, 2); err != nil {
		t.Fatalf("drop: %v", err)
	}
	h2 := newOtaHarness(t, func(c *OTARequestorConfig) { c.Store = h.store })
	if err := h2.r.load(ctx); err != nil {
		t.Fatal(err)
	}
	if held := h2.r.MatterHeldFabrics(); slices.Contains(held, 2) {
		t.Errorf("a restart loads fabric 2's active provider: held %v", held)
	}
}
