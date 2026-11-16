// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"encoding/json"
	"slices"
	"testing"

	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

func announce(t *testing.T, h *otaHarness, fabric uint8, node uint64) {
	t.Helper()
	req := otadef.AnnounceOtaProviderRequest{ProviderNodeId: node, VendorId: 0xFFF1, AnnouncementReason: otadef.AnnouncementReasonSimpleAnnouncement}
	if _, err := h.r.MatterInvoke(fabricCtx(fabric), otadef.CmdAnnounceOtaProvider, req); err != nil {
		t.Fatal(err)
	}
}

func (h *otaHarness) activeOf(fabric uint8) int {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	n := 0
	for _, a := range h.r.active {
		if a.Location.FabricIndex == fabric {
			n++
		}
	}
	return n
}

func (h *otaHarness) storedActiveOf(t *testing.T, fabric uint8) int {
	t.Helper()
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	var st persistedState
	if err := json.Unmarshal(h.store.state, &st); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range st.ActiveOtaProviders {
		if a.Location.FabricIndex == fabric {
			n++
		}
	}
	return n
}

// TestOTARequestor_FabricRemovalDropsItsProviders (review II,
// R2-P6-store-1204): a removed fabric's announced provider leaves the
// active list, the stored state, the selection and the announcements
// deferred behind a query; MatterHeldFabrics names the fabric while it
// holds an active provider, so the node's reconcile sees it.
func TestOTARequestor_FabricRemovalDropsItsProviders(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	announce(t, h, 1, 0x11)
	announce(t, h, 2, 0x22)
	if got := h.r.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Errorf("MatterHeldFabrics %v, want [1 2]", got)
	}
	h.r.mu.Lock()
	h.r.selected = &otadef.ProviderLocation{FabricIndex: 2, ProviderNodeId: 0x22}
	h.r.queryRunning = true
	h.r.mu.Unlock()
	announce(t, h, 2, 0x23)
	announce(t, h, 1, 0x12)
	h.r.FabricRemoved(2)
	if n := h.activeOf(2); n != 0 {
		t.Errorf("%d active providers of the removed fabric", n)
	}
	if n := h.storedActiveOf(t, 2); n != 0 {
		t.Errorf("%d stored active providers of the removed fabric", n)
	}
	if n := h.activeOf(1); n != 1 {
		t.Errorf("%d active providers of fabric 1, want 1", n)
	}
	h.r.mu.Lock()
	sel, deferred := h.r.selected, slices.Clone(h.r.announcedDuringQuery)
	h.r.queryRunning = false
	h.r.mu.Unlock()
	if sel != nil {
		t.Errorf("selected %+v survived its fabric's removal", sel)
	}
	if len(deferred) != 1 || deferred[0].fabric != 1 {
		t.Errorf("deferred announcements %+v, want fabric 1's alone", deferred)
	}
	if got := h.r.MatterHeldFabrics(); !slices.Equal(got, []uint8{1}) {
		t.Errorf("MatterHeldFabrics %v after the removal, want [1]", got)
	}
}

// TestOTARequestor_ActiveProvidersBoundedPerFabric (review II,
// R2-P6-bound-1205, U3-net-1, decision 134): one fabric's announcements
// hold at most otaActiveProvidersPerFabric active entries, and the
// announcements deferred behind a query collapse per provider and are held
// to the same share; another fabric's entries are untouched.
func TestOTARequestor_ActiveProvidersBoundedPerFabric(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	announce(t, h, 2, 0x99)
	for node := range uint64(50) {
		announce(t, h, 1, 0x100+node)
	}
	if n := h.activeOf(1); n != otaActiveProvidersPerFabric {
		t.Errorf("%d active providers of fabric 1 after 50 announcements, want %d", n, otaActiveProvidersPerFabric)
	}
	if n := h.activeOf(2); n != 1 {
		t.Errorf("fabric 2's provider: %d entries, want 1", n)
	}
	h.r.mu.Lock()
	h.r.queryRunning = true
	h.r.mu.Unlock()
	for range 50 {
		announce(t, h, 1, 0x200)
	}
	for node := range uint64(50) {
		announce(t, h, 3, 0x300+node)
	}
	h.r.mu.Lock()
	perFabric := map[uint8]int{}
	for _, a := range h.r.announcedDuringQuery {
		perFabric[a.fabric]++
	}
	h.r.queryRunning = false
	h.r.mu.Unlock()
	if perFabric[1] != 1 || perFabric[3] != otaActiveProvidersPerFabric {
		t.Errorf("deferred per fabric %v, want 1 for fabric 1 and %d for fabric 3", perFabric, otaActiveProvidersPerFabric)
	}
}
