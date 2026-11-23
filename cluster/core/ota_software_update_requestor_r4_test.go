// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	otadef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// r4RefusingOtaStore refuses SaveOTARequestorState while refuse is set.
type r4RefusingOtaStore struct {
	OtaRequestorStore
	refuse atomic.Bool
}

func (s *r4RefusingOtaStore) SaveOTARequestorState(ctx context.Context, b []byte) error {
	if s.refuse.Load() {
		return errors.New("store refused")
	}
	return s.OtaRequestorStore.SaveOTARequestorState(ctx, b)
}

// TestOTARequestorDropRefusedSaveStaysOwed: a fabric's drop whose save the
// store refuses keeps the fabric listed as held and answers the error; the
// reconcile's next drop stores a state without the fabric's active
// providers, so a restart does not load them (review II, R4-store-4102).
func TestOTARequestorDropRefusedSaveStaysOwed(t *testing.T) {
	var st *r4RefusingOtaStore
	h := newOtaHarness(t, func(c *OTARequestorConfig) {
		st = &r4RefusingOtaStore{OtaRequestorStore: c.Store}
		c.Store = st
	})
	ctx := context.Background()
	h.r.addActiveOtaProvider(ctx, otadef.ProviderLocation{FabricIndex: 2, ProviderNodeId: 0x77, Endpoint: 0}, nil)
	st.refuse.Store(true)
	if err := h.r.MatterDropFabric(ctx, 2); err == nil {
		t.Error("the drop whose save the store refused answered nil")
	}
	st.refuse.Store(false)
	if held := h.r.MatterHeldFabrics(); !slices.Contains(held, 2) {
		t.Fatalf("MatterHeldFabrics = %v after a refused save of the drop, want 2 listed (owed)", held)
	}
	if err := h.r.MatterDropFabric(ctx, 2); err != nil {
		t.Fatalf("second drop: %v", err)
	}
	if held := h.r.MatterHeldFabrics(); slices.Contains(held, 2) {
		t.Errorf("MatterHeldFabrics = %v after the owed save landed, want 2 gone", held)
	}
	var stored persistedState
	if err := json.Unmarshal(h.store.state, &stored); err != nil {
		t.Fatal(err)
	}
	for _, a := range stored.ActiveOtaProviders {
		if a.Location.FabricIndex == 2 {
			t.Errorf("the store still holds fabric 2's active provider after the reconcile's drop")
		}
	}
	h2 := newOtaHarness(t, func(c *OTARequestorConfig) { c.Store = h.store })
	if err := h2.r.load(ctx); err != nil {
		t.Fatal(err)
	}
	if held := h2.r.MatterHeldFabrics(); slices.Contains(held, 2) {
		t.Errorf("a restart loads fabric 2's active provider: held %v", held)
	}
}

// TestOTARequestorLaterSaveSettlesAnOwedDrop: any later save stores the
// state without the dropped fabric, which then is no longer owed.
func TestOTARequestorLaterSaveSettlesAnOwedDrop(t *testing.T) {
	var st *r4RefusingOtaStore
	h := newOtaHarness(t, func(c *OTARequestorConfig) {
		st = &r4RefusingOtaStore{OtaRequestorStore: c.Store}
		c.Store = st
	})
	ctx := context.Background()
	h.r.addActiveOtaProvider(ctx, otadef.ProviderLocation{FabricIndex: 2, ProviderNodeId: 0x77, Endpoint: 0}, nil)
	st.refuse.Store(true)
	_ = h.r.MatterDropFabric(ctx, 2)
	st.refuse.Store(false)
	h.r.addActiveOtaProvider(ctx, otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: 0x66, Endpoint: 0}, nil)
	if held := h.r.MatterHeldFabrics(); !slices.Equal(held, []uint8{1}) {
		t.Errorf("MatterHeldFabrics = %v after a later save landed, want [1]", held)
	}
}
