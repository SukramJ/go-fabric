// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestPartMovesBeforeTheNodeRuns (R2-P6-fsm-2501, decision 225): on a node
// that has not started, a part moves between two bridged nodes the
// Aggregator holds and the started node serves it under its new owner;
// on the running node the move is refused and changes nothing.
func TestPartMovesBeforeTheNodeRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	newOwner := func(key string) *device.BridgedNode {
		d, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey(key), Name: key})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b := newOwner("a"), newOwner("b")
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPart(lamp.Device); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*device.BridgedNode{a, b} {
		if err := n.Aggregator().Add(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.AddPart(lamp.Device); err != nil {
		t.Fatalf("a move before the node runs: %v", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	partsOf := func(key string) []string {
		for _, d := range n.Topology().Devices {
			if d.Key.String() == key {
				var keys []string
				for _, p := range d.Parts {
					keys = append(keys, p.Key.String())
				}
				return keys
			}
		}
		return nil
	}
	if !slices.Equal(partsOf("b"), []string{"lamp"}) || len(partsOf("a")) != 0 {
		t.Fatalf("after the move the node serves a %v, b %v; want the lamp under b", partsOf("a"), partsOf("b"))
	}
	ep, ok := lamp.EndpointNumber()
	if !ok {
		t.Fatal("the moved part has no endpoint number")
	}
	if err := a.AddPart(lamp.Device); !errors.Is(err, device.ErrComposition) {
		t.Fatalf("a move on the running node: %v, want ErrComposition", err)
	}
	if !slices.Contains(b.Parts(), lamp.Device) || len(a.Parts()) != 0 {
		t.Errorf("the refused move changed the owners: a %v, b %v", a.Parts(), b.Parts())
	}
	if got, ok := lamp.EndpointNumber(); !ok || got != ep {
		t.Errorf("the refused move changed the part's endpoint: %d, %v; was %d", got, ok, ep)
	}
}
