// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import (
	"encoding/json"
	"testing"

	"thingfabric.dev/go-fabric/parity"
)

// TestAttributeOrderMatchesSnapshot holds the generated order table
// against the snapshot it is generated from (review II,
// R2-P4-imread-12): every attribute of a cluster's attributeOrder has its
// index there as its place, every attribute of the cluster's attributes
// array is placed, a cluster the snapshot does not define places the
// global attributes by globalAttributeOrder, and an attribute no order
// lists has no place. A stale attribute_order_gen.go fails here.
func TestAttributeOrderMatchesSnapshot(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Name       string `json:"name"`
			Attributes []struct {
				ID uint32 `json:"id"`
			} `json:"attributes"`
			AttributeOrder []uint32 `json:"attributeOrder"`
		} `json:"clusters"`
		GlobalAttributeOrder []uint32 `json:"globalAttributeOrder"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Clusters) == 0 || len(snap.GlobalAttributeOrder) == 0 {
		t.Fatalf("snapshot carries %d clusters and %d global attributes in order", len(snap.Clusters), len(snap.GlobalAttributeOrder))
	}
	for _, c := range snap.Clusters {
		if len(attributeOrder[c.ID]) != len(c.AttributeOrder) {
			t.Errorf("%s: %d attributes ranked, the snapshot orders %d", c.Name, len(attributeOrder[c.ID]), len(c.AttributeOrder))
		}
		for i, id := range c.AttributeOrder {
			if rank, ok := AttributeOrder(c.ID, id); !ok || rank != i {
				t.Errorf("%s: AttributeOrder(0x%04X) = %d, %v; want %d, true", c.Name, id, rank, ok, i)
			}
		}
		for _, a := range c.Attributes {
			if _, ok := AttributeOrder(c.ID, a.ID); !ok {
				t.Errorf("%s: attribute 0x%04X has no place in the order", c.Name, a.ID)
			}
		}
		if _, ok := AttributeOrder(c.ID, 0x1234_0000); ok {
			t.Errorf("%s: a vendor attribute has a place in the order", c.Name)
		}
	}
	const hostCluster = 0xFFF1_FC01
	for i, id := range snap.GlobalAttributeOrder {
		if rank, ok := AttributeOrder(hostCluster, id); !ok || rank != i {
			t.Errorf("host cluster: AttributeOrder(0x%04X) = %d, %v; want %d, true", id, rank, ok, i)
		}
	}
	if _, ok := AttributeOrder(hostCluster, 0x0000); ok {
		t.Error("host cluster: its own attribute 0x0000 has a place in the order")
	}
}
