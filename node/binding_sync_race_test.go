// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec/binding"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
)

// lightSwitch is an on/off light switch under key: a device with a
// Binding server.
func lightSwitch(t *testing.T, key string) *device.OnOffLightSwitch {
	t.Helper()
	sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey(key), Name: key})
	if err != nil {
		t.Fatal(err)
	}
	return sw
}

// TestBindingSyncFollowsTheNewestTopology (review II, R3-conc-3752): a
// reassembly that lands while Start attaches the Binding servers — after
// it read the topology, before it synced — is not undone by Start's sync
// of the older topology: the binding manager ends on the Binding servers
// of the topology the bridge serves.
func TestBindingSyncFollowsTheNewestTopology(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "binding-sync.db"))))
	if err := n.Aggregator().Add(ctx, lightSwitch(t, "first")); err != nil {
		t.Fatal(err)
	}
	added := make(chan error, 1)
	once := false
	node.BeforeStartBindingSync(n, func() {
		if once {
			return
		}
		once = true
		go func() { added <- n.Aggregator().Add(ctx, lightSwitch(t, "second")) }()
		// The Add's own sync runs now; a sync that waits for Start's lets
		// the Add finish after it instead.
		select {
		case err := <-added:
			added <- err
		case <-time.After(300 * time.Millisecond):
		}
	})
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()
	if err := <-added; err != nil {
		t.Fatal(err)
	}
	var want []uint16
	for _, e := range n.Endpoints() {
		for _, c := range e.Clusters {
			if c.ID == binding.ClusterID {
				want = append(want, e.Endpoint)
			}
		}
	}
	got := node.AttachedBindings(n)
	slices.Sort(got)
	slices.Sort(want)
	if len(want) < 2 {
		t.Fatalf("the served topology has Binding servers on %v, want both switches'", want)
	}
	if !slices.Equal(got, want) {
		t.Errorf("binding manager attached %v, the served topology's Binding servers are on %v", got, want)
	}
}
