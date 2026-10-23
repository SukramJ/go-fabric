// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestReportStatusResponseAckedAtOnce pins the device's acknowledgement
// of the controller's StatusResponse to a subscription report: it is the
// last message of the device's report exchange, so the device sends its
// standalone ack at once — matter.js MessageExchange.ts:1117-1124 at
// 862ad75f (close acks the last received message immediately) — and not
// after the ack pump's grace window. The site's fake clock stands still
// once the report has arrived; before the fix the controller's
// StatusResponse waited on an ack that only that clock would release,
// and the controller's teardown ran into its 10 s bound.
func TestReportStatusResponseAckedAtOnce(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "plug"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ep, _ := plug.EndpointNumber()
	var off atomic.Bool
	sub, err := fabrictest.Subscribe(ctx, pair, fabrictest.SubscribeRequest{
		Attributes: []fabrictest.AttributePath{client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		Updated: func(r *fabrictest.Report) {
			if v, ok := r.Value(ep, onoffdef.ClusterID, onoffdef.AttrOnOff); ok && !v.El.Bool {
				off.Store(true)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pair.Device.Act(func(*node.Node) error { return plug.OnOff().Set(onoffdef.Patch{OnOff: new(false)}) }); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the report", off.Load); err != nil {
		t.Fatal(err)
	}
	// The clock stands still from here: the ack must not need it.
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	sub.Close()
	start := time.Now()
	pair.Controller.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the controller's teardown took %v: a StatusResponse still waited for the device's ack", d)
	}
}
