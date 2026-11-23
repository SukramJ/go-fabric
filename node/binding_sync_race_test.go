// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

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

// goroutineWaitsIn reports whether a goroutine whose stack holds frame is
// blocked — on a lock, a channel, a select — rather than running,
// runnable or in a system call: read from the runtime's own stack dump.
func goroutineWaitsIn(frame string) bool {
	buf := make([]byte, 1<<20)
	for _, g := range strings.Split(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
		header, _, _ := strings.Cut(g, "\n")
		_, state, _ := strings.Cut(header, " [")
		if !strings.Contains(g, frame) {
			continue
		}
		if !strings.HasPrefix(state, "running") && !strings.HasPrefix(state, "runnable") && !strings.HasPrefix(state, "syscall") {
			return true
		}
	}
	return false
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
		// the Add finish after it instead: wait until the Add returned or
		// waits on something — no wall-clock wait.
		for {
			select {
			case err := <-added:
				added <- err
				return
			default:
			}
			if goroutineWaitsIn("node.(*Aggregator).Add(") {
				return
			}
			runtime.Gosched()
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
