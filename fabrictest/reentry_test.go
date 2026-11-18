// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestNetworkFilterMayChangeTheNetwork (R2-P6-conc-2520): the filter runs
// outside the network's lock, so a filter that reads or sets the
// network's options returns.
func TestNetworkFilterMayChangeTheNetwork(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	nw := site.Net()
	nw.Filter(func(fabrictest.Datagram) bool {
		nw.Set(nw.Options())
		return true
	})
	t.Cleanup(func() { nw.Filter(nil) })
	// A filter run under the network's lock deadlocks here (go test's
	// timeout).
	site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{newPlug(t)}}})
}

// TestActOnAStoppedDeviceIsRefused (R2-P6-conc-2520): Act runs fn on a
// running node only; a stopped device's node is not handed out.
func TestActOnAStoppedDeviceIsRefused(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{newPlug(t)}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := d.Act(func(*node.Node) error { ran = true; return nil }); err == nil || ran {
		t.Errorf("Act on a stopped device: err %v, fn ran %v; want a refusal without fn", err, ran)
	}
}
