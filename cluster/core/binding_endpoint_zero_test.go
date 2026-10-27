// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
)

// TestBinding_EndpointAndClusterZeroArePresent: a controller's entry naming
// endpoint 0 (the root endpoint) or cluster 0 (Identify) keeps the field
// present — matter.js's TargetStruct fields are optional, not sentinel
// coded (packages/model/src/standard/elements/binding.element.ts at
// 0ceb202d) — so the entry is not read as a node binding without a
// target endpoint.
func TestBinding_EndpointAndClusterZeroArePresent(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	node, endpoint, cluster := uint64(0x11), uint16(0), uint32(0)
	write := []bindingdef.TargetStruct{{Node: &node, Endpoint: &endpoint, Cluster: &cluster, FabricIndex: 1}}
	if err := b.MatterWrite(context.Background(), bindingdef.AttrBinding, write); err != nil {
		t.Fatal(err)
	}
	e := coreEntries(b)
	if len(e) != 1 || !e[0].HasEndpoint() || e[0].Endpoint != 0 || !e[0].HasCluster() || e[0].Cluster != 0 {
		t.Fatalf("entries %+v: endpoint 0 / cluster 0 read as absent", e)
	}
	if got, want := e[0].Key(), "1/17//0/0"; got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
	// Absent stays absent.
	if (core.TargetStruct{Node: 1}).HasEndpoint() || (core.TargetStruct{Node: 1, Cluster: 0xFFFFFFFF, ClusterPresent: true}).HasCluster() {
		t.Fatal("an absent field reads as present")
	}
}
