// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
)

// TestImageQuotaChargesTheStructureADescriptorListCreates (review II,
// R2-P6-bound-2351, owner decision 54): a report whose ServerList names
// clusters, or whose PartsList names endpoints, the node does not have is
// charged what applying it would create. One report no longer leaves the
// image above its quota: it is refused whole. Before, only the clusters
// with values were charged, and a root ServerList of 20 002 ids left a
// 4 MiB quota's image at 10.3x the quota.
func TestImageQuotaChargesTheStructureADescriptorListCreates(t *testing.T) {
	t.Parallel()
	const quota = 4 << 20
	const entries = 20000
	for _, tc := range []struct {
		name string
		list func() (uint32, []uint64)
	}{
		{"ServerList ids", func() (uint32, []uint64) {
			ids := []uint64{uint64(descriptor.ClusterID), uint64(clBasicInfo)}
			for k := range entries {
				ids = append(ids, uint64(0x0001_0000+k))
			}
			return descriptor.AttrServerList, ids
		}},
		{"PartsList parts", func() (uint32, []uint64) {
			ids := make([]uint64, 0, entries)
			for k := range entries {
				ids = append(ids, uint64(100+k))
			}
			return descriptor.AttrPartsList, ids
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, _ := quotaNode(t, &MemoryStore{}, quota)
			n.apply(wildcard, &bridge(1).r, true)
			before := n.s.imageBytes()
			attr, ids := tc.list()
			b := &report{}
			b.attr(0, descriptor.ClusterID, attr, listNode(ids...), 2)
			n.apply(wildcard, &b.r, true)
			after := n.s.imageBytes()
			if after > quota {
				t.Fatalf("one admitted report left the image at %d bytes, %.1fx the quota %d", after, float64(after)/quota, quota)
			}
			if after != before {
				t.Fatalf("image %d bytes after the refused report, want %d (nothing applied)", after, before)
			}
		})
	}
}

// TestImageQuotaAdmitsADescriptorListWithinIt: a ServerList and a
// PartsList whose new structure fits the quota are applied.
func TestImageQuotaAdmitsADescriptorListWithinIt(t *testing.T) {
	t.Parallel()
	n, _ := quotaNode(t, &MemoryStore{}, 4<<20)
	n.apply(wildcard, &bridge(1).r, true)
	b := &report{}
	b.attr(0, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), uint64(clBasicInfo), 0x0001_0000), 2)
	n.apply(wildcard, &b.r, true)
	if n.s.endpoints[0].clusters[0x0001_0000] == nil {
		t.Fatal("the ServerList's new cluster was not created")
	}
}
