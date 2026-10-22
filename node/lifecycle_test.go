// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func lifecycleAttr(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// TestDeviceReachableAndRename: a controller reads Reachable flip with the
// ReachableChanged event a SetReachable fires (matter.js
// BridgedDeviceBasicInformationServer.ts:37, :100-102), and the NodeLabel
// a Rename sets; the device knows its endpoint number once it is added.
func TestDeviceReachableAndRename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lamp.EndpointNumber(); ok {
		t.Error("an endpoint number before the device is added")
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{lamp}}})
	ep, ok := lamp.EndpointNumber()
	if want, _ := pair.Device.EndpointOf("lamp"); !ok || ep != want {
		t.Fatalf("EndpointNumber = %d, %v; want %d", ep, ok, want)
	}

	if err := pair.Device.Act(func(*node.Node) error { lamp.SetReachable(ctx, false); return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := fabrictest.Read(ctx, pair, lifecycleAttr(ep, bdbidef.ClusterID, bdbidef.AttrReachable))
	if err != nil || got.Value.El.Bool {
		t.Fatalf("Reachable = %+v, %v; want false", got.Value.El, err)
	}
	events, err := fabrictest.Events(ctx, pair, im.ConcreteEventPath{
		Endpoint: ep, Cluster: bdbidef.ClusterID, Event: bdbidef.EventReachableChanged,
		HasEndpoint: true, HasCluster: true, HasEvent: true,
	})
	if err != nil || len(events) != 1 || len(events[0].Value.Children) == 0 || events[0].Value.Children[0].El.Bool {
		t.Fatalf("ReachableChanged events %+v, %v; want one, reachableNewValue false", events, err)
	}
	if lamp.Reachable() {
		t.Error("Reachable() after SetReachable(false)")
	}

	if err := pair.Device.Act(func(*node.Node) error { lamp.Rename(ctx, "Desk lamp"); return nil }); err != nil {
		t.Fatal(err)
	}
	got, err = fabrictest.Read(ctx, pair, lifecycleAttr(ep, bdbidef.ClusterID, bdbidef.AttrNodeLabel))
	if err != nil || got.Value.El.String != "Desk lamp" {
		t.Fatalf("NodeLabel = %+v, %v; want the rename", got.Value.El, err)
	}
	topo := pair.Device.Node().Topology()
	if len(topo.Devices) != 1 || topo.Devices[0].Name != "Desk lamp" || topo.Devices[0].Reachable || topo.Devices[0].Endpoint != ep {
		t.Errorf("Topology %+v, want the renamed, unreachable lamp on %d", topo, ep)
	}
}

// TestAggregatorReplaceAndTopology: a Replace keeps the endpoint number of
// the key and changes the endpoint's Descriptor in one report (matter.js
// closes the part, keeping its number, and adds the new one under the same
// id: endpoint/Endpoint.ts:1024-1032, ServerEndpointInitializer.ts:34-42, :67-69);
// Topology follows Add, Replace and Remove.
func TestAggregatorReplaceAndTopology(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	opts := func(key string) device.Options {
		return device.Options{StableKey: endpoint.StringKey(key), Name: key}
	}
	lamp, err := device.NewOnOffLight(opts("lamp"))
	if err != nil {
		t.Fatal(err)
	}
	plug, err := device.NewOnOffPlugInUnit(opts("plug"))
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{lamp, plug}}})
	n := pair.Device.Node()
	before, _ := lamp.EndpointNumber()
	keys := func() []string {
		devices := n.Topology().Devices
		out := make([]string, 0, len(devices))
		for _, d := range devices {
			out = append(out, d.Key.String())
		}
		return out
	}
	if got := keys(); len(got) != 2 {
		t.Fatalf("Topology after Add: %v", got)
	}

	types := lifecycleAttr(before, descdef.ClusterID, descdef.AttrDeviceTypeList)
	reports := make(chan *interaction.Report, 8)
	sub, err := fabrictest.Subscribe(ctx, pair, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{types},
		Updated:    func(r *interaction.Report) { reports <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	dimmer, err := device.NewDimmableLight(opts("lamp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Replace(ctx, endpoint.StringKey("lamp"), dimmer); err != nil {
		t.Fatal(err)
	}
	if after, ok := dimmer.EndpointNumber(); !ok || after != before {
		t.Fatalf("the replacement serves %d (%v), want the old number %d", after, ok, before)
	}
	if _, ok := lamp.EndpointNumber(); ok {
		t.Error("the replaced device still answers an endpoint number")
	}
	var seen int
	if err := site.Eventually(ctx, "the Descriptor report", func() bool {
		select {
		case r := <-reports:
			if _, ok := r.Value(before, descdef.ClusterID, descdef.AttrDeviceTypeList); ok {
				seen++
			}
		default:
		}
		return seen > 0
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	for len(reports) > 0 {
		if r := <-reports; r != nil {
			if _, ok := r.Value(before, descdef.ClusterID, descdef.AttrDeviceTypeList); ok {
				seen++
			}
		}
	}
	if seen != 1 {
		t.Errorf("the Replace reported the Descriptor %d times, want once", seen)
	}
	got, err := fabrictest.Read(ctx, pair, types)
	if err != nil || len(got.Value.Children) == 0 || got.Value.Children[0].Children[0].El.Uint != uint64(device.DimmableLightID) {
		t.Errorf("DeviceTypeList after the Replace %+v, %v; want DimmableLight", got.Value, err)
	}

	if err := n.Aggregator().Replace(ctx, endpoint.StringKey("lamp"), plug); !errors.Is(err, node.ErrKeyMismatch) {
		t.Errorf("Replace with another key: %v, want ErrKeyMismatch", err)
	}
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("plug")); err != nil {
		t.Fatal(err)
	}
	if got := keys(); !slices.Equal(got, []string{"lamp"}) {
		t.Errorf("Topology after Remove: %v, want [lamp]", got)
	}
	if d := n.Topology().Devices[0]; d.Added != node.Endpoint(dimmer) || d.DeviceType != uint16(device.DimmableLightID) {
		t.Errorf("Topology's device %+v, want the replacement", d)
	}
}
