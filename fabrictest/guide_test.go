// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestPlugToggles is the first test of docs/guides/testing-in-process.md,
// as the page shows it, so the page cannot drift from the API.
func TestPlugToggles(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	plug, err := device.NewOnOffPlugInUnit(device.Options{
		StableKey: endpoint.StringKey("plug:1"), Name: "plug",
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}},
	})
	ep, _ := plug.EndpointNumber()
	// A deadline of the test's own: a wait without one ends after the
	// site's (Options.Wait, 30 s on the wall clock) with ErrWaited.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	toggle := im.ConcreteCommandPath{Endpoint: ep, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	onOff := im.ConcreteAttributePath{
		Endpoint: ep, Cluster: onoffdef.ClusterID, Attribute: onoffdef.AttrOnOff,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	got, err := fabrictest.Read(ctx, pair, onOff)
	if err != nil || !got.Value.El.Bool {
		t.Fatalf("OnOff %+v, %v", got, err)
	}

	// The page's Time section: a change made locally reaches the
	// subscriber once the clock passes the subscription's floor.
	reports := make(chan *interaction.Report, 8)
	sub, err := fabrictest.Subscribe(ctx, pair, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff},
		Updated:    func(r *interaction.Report) { reports <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	reported := func() bool {
		select {
		case r := <-reports:
			v, ok := r.Value(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)
			return ok && !v.El.Bool
		default:
			return false
		}
	}
	if err := pair.Device.Act(func(*node.Node) error { return plug.OnOff().Set(onoffdef.Patch{OnOff: new(false)}) }); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the report", reported); err != nil {
		t.Fatal(err)
	}
}
