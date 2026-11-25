// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/internal/specseam"
)

// TestSetAttributesStoredRunsBetweenStoreAndNotification (PR 1 review M1,
// decision 682): the seam's stored step runs once the value is stored —
// a read inside it sees the new value — and before the change listener
// is told; a Set the server refuses runs it not at all, and a server that
// is not a *spec.Server is an error.
func TestSetAttributesStoredRunsBetweenStoreAndNotification(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	var order []string
	srv.OnAttributeChange(func(contract.Change) { order = append(order, "told") })
	stored := func() {
		v, _ := srv.MatterRead(hepa.AttrCondition)
		if v != uint8(41) {
			t.Errorf("Condition inside the stored step = %v, want the stored 41", v)
		}
		order = append(order, "stored")
	}
	if err := specseam.SetAttributesStored(srv, contract.OriginHostSet, map[uint32]any{hepa.AttrCondition: uint8(41)}, stored); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "stored" || order[1] != "told" {
		t.Fatalf("order %v, want [stored told]", order)
	}
	order = nil
	if err := specseam.SetAttributesStored(srv, contract.OriginHostSet, map[uint32]any{hepa.AttrCondition: uint8(101)}, stored); err == nil {
		t.Fatal("Condition 101 accepted, want the bound's refusal")
	}
	if len(order) != 0 {
		t.Fatalf("a refused Set ran %v, want nothing", order)
	}
	if err := specseam.SetAttributesStored(struct{}{}, contract.OriginHostSet, nil, stored); err == nil {
		t.Fatal("a server that is not a *spec.Server accepted")
	}
}
