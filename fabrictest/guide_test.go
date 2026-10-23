// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestPlugToggles is the first test of docs/guides/testing-in-process.md,
// as the page shows it, so the page cannot drift from the API.
func TestPlugToggles(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	plug, err := device.NewOnOffPlugInUnit(device.Options{
		StableKey: device.StringKey("plug:1"), Name: "plug",
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

	toggle := client.CommandAt(ep, onoffdef.ClusterID, onoffdef.CmdToggle)
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	values, err := fabrictest.ReadCluster(ctx, pair, ep, onoffdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := onoffdef.StateFrom(values); !state.OnOff {
		t.Fatalf("OnOff %+v", state)
	}

	// The page's Time section: a change made locally reaches the
	// subscriber once the clock passes the subscription's floor.
	reports := make(chan *fabrictest.Report, 8)
	sub, err := fabrictest.Subscribe(ctx, pair, fabrictest.SubscribeRequest{
		Attributes: []fabrictest.AttributePath{client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		Updated:    func(r *fabrictest.Report) { reports <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	reported := func() bool {
		select {
		case r := <-reports:
			_, ok := r.Value(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)
			state, _ := onoffdef.StateFrom(r.Values(ep, onoffdef.ClusterID))
			return ok && !state.OnOff
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
