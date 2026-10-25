// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestReadClusterOnARemovedEndpoint: ReadCluster's path names an endpoint
// and a cluster with every attribute, a wildcard path the device skips
// without a status where it matches nothing, so a removed endpoint reads
// as no values and no error, as the testing guide says.
func TestReadClusterOnARemovedEndpoint(t *testing.T) {
	t.Parallel()
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "plug"})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}}})
	ep, _ := plug.EndpointNumber()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if v, err := fabrictest.ReadCluster(ctx, pair, ep, onoffdef.ClusterID); err != nil || len(v) == 0 {
		t.Fatalf("before the removal: %d values, %v", len(v), err)
	}
	if err := pair.Device.Act(func(n *node.Node) error { return n.Aggregator().Remove(ctx, device.StringKey("plug")) }); err != nil {
		t.Fatal(err)
	}
	if v, err := fabrictest.ReadCluster(ctx, pair, ep, onoffdef.ClusterID); err != nil || len(v) != 0 {
		t.Fatalf("after the removal: %d values, %v; want none and no error", len(v), err)
	}
}
