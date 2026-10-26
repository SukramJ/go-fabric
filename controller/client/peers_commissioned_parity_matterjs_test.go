// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
)

// TestPeersCommissionedParity ports matter.js's PeersCommissionedTest
// (packages/node/test/node/client/PeersCommissionedTest.ts at 862ad75f):
// Commissioned holds the commissioned node and an operational node a peer
// address created, never a group node (Peers.ts:410-413). The Go registry
// holds no commissionable discoveries — client/discovery reports them
// without registering a node — so that third exclusion has nothing to
// exclude here.
func TestPeersCommissionedParity(t *testing.T) {
	t.Parallel()
	cp := newClientPair(t, clientPairOptions{})
	reg := cp.pair.Controller.Registry()
	addr, ok := cp.node.PeerAddress()
	if !ok {
		t.Fatal("not commissioned")
	}
	group, err := reg.ForAddress(client.PeerAddress{FabricIndex: addr.FabricIndex, NodeID: 0xFFFF_FFFF_FFFF_0001}, "")
	if err != nil {
		t.Fatal(err)
	}
	operational, err := reg.ForAddress(client.PeerAddress{FabricIndex: addr.FabricIndex, NodeID: 0x99}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !operational.IsCommissioned() {
		t.Fatal("the operational node a peer address created is not commissioned")
	}
	got := map[*client.Node]bool{}
	for _, n := range reg.Commissioned() {
		got[n] = true
	}
	if len(got) != 2 || !got[cp.node] || !got[operational] || got[group] {
		t.Fatalf("commissioned %v, want the commissioned and the operational node", reg.Commissioned())
	}
}
