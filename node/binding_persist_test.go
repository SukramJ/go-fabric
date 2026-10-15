// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// TestNodeBindingPersisted: the Binding list of a switch is kept across a
// restart on the node's database (the attribute is nonvolatile,
// binding.element.ts:21-25): a write is restored by the next node.New on
// the database, before the binding manager attaches the server; a removed
// fabric's entries stay removed.
func TestNodeBindingPersisted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	boot := func() (*node.Node, *mattercore.Binding) {
		t.Helper()
		n := newNode(t, minimalConfig(db))
		sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch"), Name: "switch"})
		if err != nil {
			t.Fatal(err)
		}
		if err := n.Aggregator().Add(ctx, sw); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return n, sw.Binding()
	}

	n, b := boot()
	entries := []mattercore.TargetStruct{
		{Node: 2, Endpoint: 3, Cluster: 6, FabricIndex: 1},
		{Group: 0x0101, FabricIndex: 1},
		{Node: 4, Endpoint: 1, FabricIndex: 2},
	}
	if err := b.MatterWrite(ctx, bindingdef.AttrBinding, entries); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	n, b = boot()
	if got := b.Entries(); !slices.Equal(got, entries) {
		t.Fatalf("restored %+v, want %+v", got, entries)
	}
	node.RemoveFabricBindings(n, 1)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	_, b = boot()
	if got := b.Entries(); !slices.Equal(got, entries[2:]) {
		t.Fatalf("after RemoveFabric restored %+v, want %+v", got, entries[2:])
	}
}
