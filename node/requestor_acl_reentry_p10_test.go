// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
)

// TestWebRTCRequestorGrantFabricReentersAggregator: a GrantFabric that
// calls back into the node's Aggregator — here a Remove of another
// endpoint — from the reassembly that asks it returns, and the requestor's
// entry is installed in each granted fabric (review II, P10-conc-6501,
// P10-conc-6003). The facade asks GrantFabric after the reassembly lock is
// released, so the callback's own reassembly takes it.
func TestWebRTCRequestorGrantFabricReentersAggregator(t *testing.T) {
	ctx := context.Background()
	s := newRequestorACLSite(t)
	cfg := s.cfg()
	var n *node.Node
	reenter := false
	removed := make(chan error, 1)
	cfg.WebRTCRequestor = &node.WebRTCRequestorConfig{GrantFabric: func(node.FabricInfo) bool {
		if reenter {
			reenter = false
			removed <- n.Aggregator().Remove(ctx, endpoint.StringKey("other:1"))
		}
		return true
	}}
	n = newNode(t, cfg)
	// other serves no requestor, so before ctl's Add no fabric holds the
	// entry: the assertion at the end pins the ensure after ctl's reassembly.
	other, err := device.NewContactSensor(device.Options{StableKey: endpoint.StringKey("other:1"), Name: "o"})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"})
	if err != nil {
		t.Fatal(err)
	}
	reenter = true
	done := make(chan error, 1)
	go func() { done <- n.Aggregator().Add(ctx, ctl) }()
	// The test's own deadline, less a margin to name the failure: no
	// window of its own (TestSleepRatchet).
	wait := ctx
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		wait, cancel = context.WithDeadline(ctx, dl.Add(-5*time.Second))
		defer cancel()
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
	case <-wait.Done():
		t.Fatal("Add did not return: GrantFabric's Aggregator call waits on the reassembly lock the reassembly that asks it holds")
	}
	select {
	case err := <-removed:
		if err != nil {
			t.Errorf("GrantFabric's Remove: %v", err)
		}
	default:
		t.Error("GrantFabric was not asked after ctl's reassembly")
	}
	if got := s.holding(); !slices.Equal(got, s.all) {
		t.Errorf("fabrics %v hold the requestor's entry, want %v", got, s.all)
	}
}
