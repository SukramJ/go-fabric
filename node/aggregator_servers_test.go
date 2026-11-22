// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/node"
)

// TestAggregatorServersRefuseADuplicateCluster (review II,
// R2-P6-api-2408): Config.AggregatorServers is held to the rule
// Config.RootServers is — a cluster the Aggregator serves already (its
// Identify, Descriptor, FixedLabel, UserLabel) or one named twice fails
// New, so endpoint 1 never mounts two servers of one cluster.
func TestAggregatorServersRefuseADuplicateCluster(t *testing.T) {
	t.Parallel()
	for name, servers := range map[string][]contract.ClusterServer{
		"the Aggregator's Identify": {mattercore.NewIdentify()},
		"one cluster twice":         {mattercore.NewBinding(), mattercore.NewBinding()},
	} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "agg.db")))
		cfg.AggregatorServers = servers
		n, err := node.New(context.Background(), cfg)
		if err == nil {
			_ = n.Stop(context.Background())
			t.Errorf("%s: New accepted it", name)
			continue
		}
		if !strings.Contains(err.Error(), "Config.AggregatorServers") {
			t.Errorf("%s: New = %v, want the AggregatorServers refusal", name, err)
		}
	}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "agg-ok.db")))
	cfg.AggregatorServers = []contract.ClusterServer{mattercore.NewBinding()}
	newNode(t, cfg)
}
