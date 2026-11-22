// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// TestFabricUpdateWithAFailedReloadStillEndsTheFabricsSessions: an
// UpdateNOC whose identity reload fails still ends the fabric's sessions
// (and aborts its pending handshakes, the same bridge call): what the
// update replaced is gone whether or not the new identity could be read
// (review II, dev6a "seen, not judged", node.go fabricUpdated) — and the
// identity in memory is withdrawn, so the node answers no CASE under an
// identity the store does not hold, as connectedhomeip never serves one
// it cannot match with the stored fabric (FabricTable.cpp:2064-2070 at
// 6170af84; Fable D3).
func TestFabricUpdateWithAFailedReloadStillEndsTheFabricsSessions(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := tcpSite(t)
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("l:1"), Name: "l"})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	n := p.Device.Node()
	fabric := oneFabric(t, p.Device)
	onFabric := func() int {
		c := 0
		for _, s := range n.Sessions().Sessions() {
			if s.FabricIndex == fabric.FabricIndex {
				c++
			}
		}
		return c
	}
	if onFabric() == 0 {
		t.Fatal("no session on the commissioned fabric to end")
	}
	// The stored identity no longer parses: the reload fails.
	rec, err := n.Store().GetIdentity(ctx, fabric.FabricIndex)
	if err != nil {
		t.Fatal(err)
	}
	rec.PrivateKey = []byte{0x01}
	if err := n.Store().UpsertIdentity(ctx, rec); err != nil {
		t.Fatal(err)
	}
	node.FabricUpdated(ctx, n, fabric.FabricIndex)
	if got := onFabric(); got != 0 {
		t.Errorf("%d session(s) of the updated fabric outlived an update whose reload failed", got)
	}
	if node.HasCASEIdentity(n, fabric.FabricIndex) {
		t.Error("the node still answers CASE for the fabric under the identity the failed reload left")
	}
}
