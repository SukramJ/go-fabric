// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func attr(ep uint16, cluster, id uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: id, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestHostViewOverTheWire commissions the two devices on an in-process
// controller: the door's state the host set reaches it, and a Toggle
// reaches the host's handler, which the host's own changes do not.
func TestHostViewOverTheWire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	var mu sync.Mutex
	var switched []bool
	b, err := newBridge(func(on bool) {
		mu.Lock()
		switched = append(switched, on)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{b.light, b.door}}})
	doorEP, ok := b.door.EndpointNumber()
	if !ok {
		t.Fatal("the door has no endpoint number")
	}
	if err := pair.Device.Act(func(*node.Node) error { return b.doorMoved(false) }); err != nil {
		t.Fatal(err)
	}
	got, err := fabrictest.Read(ctx, pair, attr(doorEP, booleanstatedef.ClusterID, booleanstatedef.AttrStateValue))
	if err != nil || got.Value.El.Bool {
		t.Fatalf("StateValue = %+v, %v; want the host's false", got.Value.El, err)
	}
	if err := pair.Device.Act(func(*node.Node) error {
		return b.light.OnOff().Set(onoffdef.Patch{OnOff: new(false)})
	}); err != nil {
		t.Fatal(err)
	}
	lightEP, _ := b.light.EndpointNumber()
	toggle := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(switched) != 1 || !switched[0] {
		t.Errorf("the handler saw %v, want the controller's one switch-on", switched)
	}
}

// TestHostViewNode starts the program's node on loopback: both devices
// are in its Topology under the Aggregator.
func TestHostViewNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, err := newBridge(func(bool) {})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config(filepath.Join(t.TempDir(), "host-view.db"))
	cfg.Network = node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Disabled: true}}
	n, err := newNode(ctx, cfg, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()
	topo := n.Topology()
	if topo.Aggregator == 0 || len(topo.Devices) != 2 {
		t.Fatalf("Topology %+v, want the Aggregator and two devices", topo)
	}
	b.light.Rename(ctx, "Kitchen ceiling")
	for _, d := range n.Topology().Devices {
		if d.Key.String() == "light" && d.Name != "Kitchen ceiling" {
			t.Errorf("the light serves the name %q after Rename", d.Name)
		}
	}
}
