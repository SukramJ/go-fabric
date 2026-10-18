// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientBridgeStructureTest.ts
// at 862ad75f, the case over a real bridge (bridgeSite, :34-65): the
// Aggregator with a composed device (a BridgedNode with two temperature
// sensors as its parts) and a light, commissioned and read by the
// controller's client node. The report-fed cases are in
// client_bridge_structure_parity_matterjs_internal_test.go.
package client_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

func TestClientBridgeStructureBridgeParity(t *testing.T) {
	t.Parallel()
	t.Run("puts a composed device's parts below it rather than below the aggregator", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		opts := func(key string) device.Options {
			return device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: site.Clock()}
		}
		composed, err := device.NewBridgedNode(opts("composed"))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"composed:sensor1", "composed:sensor2"} {
			s, err := device.NewTemperatureSensor(opts(key))
			if err != nil {
				t.Fatal(err)
			}
			if err := composed.AddPart(s.Device); err != nil {
				t.Fatal(err)
			}
		}
		light, err := newLight(site, "light")
		if err != nil {
			t.Fatal(err)
		}
		p := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{composed, light}}})
		n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, client.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		compEP, _ := p.Device.EndpointOf("composed")
		if err := site.Eventually(parityCtx(t), "the composed device's parts", func() bool {
			c := n.Endpoint(compEP)
			return c != nil && len(c.Parts()) == 2
		}); err != nil {
			t.Fatal(err)
		}
		agg := n.Endpoint(1)
		if agg == nil || len(agg.Parts()) != 2 {
			t.Fatalf("aggregator parts %d, want the composed device and the light", len(agg.Parts()))
		}
		for _, part := range n.Endpoint(compEP).Parts() {
			if part.Owner() == nil || part.Owner().Number() != compEP {
				t.Fatalf("part %d below %v", part.Number(), part.Owner())
			}
		}
	})
}
