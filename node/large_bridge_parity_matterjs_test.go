// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/endpoints/LargeBridgeTest.ts at
// 862ad75f: a bridge with many bridged lights instantiates and closes, and
// a commissioned controller reads it whole in bounded time.
//
// matter.js builds an AggregatorEndpoint with NODE_COUNT BridgedLightDevice
// parts (:17-37; its TODO asks for 1000+) and reads every attribute over
// interaction.read (:46-53); the port adds NODE_COUNT device-kit on/off
// lights to the node's Aggregator on a fabrictest site and reads the whole
// node with a wildcard read over the wire. The time bound is the test's
// deadline: matter.js's "reasonable amount of time" is its test timeout.
package node_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// largeBridgeNodeCount is matter.js's NODE_COUNT (LargeBridgeTest.ts:17).
const largeBridgeNodeCount = 10

func largeBridge(t *testing.T) []node.Endpoint {
	t.Helper()
	eps := make([]node.Endpoint, 0, largeBridgeNodeCount)
	for i := range largeBridgeNodeCount {
		key := fmt.Sprintf("light-%d", i)
		light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey(key), Name: key})
		if err != nil {
			t.Fatal(err)
		}
		eps = append(eps, light)
	}
	return eps
}

func TestLargeBridgeParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("instantiates and destroys", func(t *testing.T) {
		t.Parallel()
		site := fabrictest.NewSite(t, fabrictest.Options{})
		d := site.AddDevice(fabrictest.Topology{Bridged: largeBridge(t)}) // started
		if got := len(d.Node().Topology().Devices); got != largeBridgeNodeCount {
			t.Errorf("%d bridged devices, want %d", got, largeBridgeNodeCount)
		}
		if err := d.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("reads in a reasonable amount of time", func(t *testing.T) {
		t.Parallel()
		site := fabrictest.NewSite(t, fabrictest.Options{})
		pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: largeBridge(t)}})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		start := time.Now()
		// The whole node: every attribute of every endpoint, as
		// interaction.read with no path reads it (:51).
		rep, err := fabrictest.ReadWith(ctx, pair, fabrictest.ReadRequest{Attributes: []fabrictest.AttributePath{{}}})
		if err != nil {
			t.Fatal(err)
		}
		endpoints := map[uint16]bool{}
		for _, a := range rep.Attributes {
			endpoints[a.Path.Endpoint] = true
		}
		// The root, the Aggregator and every light.
		if len(endpoints) != largeBridgeNodeCount+2 {
			t.Errorf("the read covers %d endpoints, want %d", len(endpoints), largeBridgeNodeCount+2)
		}
		t.Logf("read %d attributes of %d endpoints in %v", len(rep.Attributes), len(endpoints), time.Since(start))
	})
}
