// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"
)

// TestICDPeerLeavesTheTableWithItsDeletedNode (review II,
// R2-P6-store-2354): a registered ICD node the registry deletes without a
// decommission — Forget here — leaves the fabric's ICD table with it: no
// entry, key or Check-In handler of it is kept, and no wakefulness.
// Before, only a decommission removed it, and the forgotten peer's
// Check-Ins were taken on a closed client.
func TestICDPeerLeavesTheTableWithItsDeletedNode(t *testing.T) {
	t.Parallel()
	p := newICDPair(t, icdPairOptions{})
	p.register(t, uncovered)
	addr := p.peerAddr(t)
	table := p.table(t)
	if _, ok := table.PeerFor(addr.NodeID); !ok {
		t.Fatal("the registered peer is not in the table: the test shows nothing")
	}
	if err := p.do(t, func(ctx context.Context) error { return p.reg.Forget(ctx, p.node) }); err != nil {
		t.Fatal(err)
	}
	if _, ok := table.PeerFor(addr.NodeID); ok {
		t.Fatal("the forgotten node's peer is still in the fabric's ICD table")
	}
	if w := table.WakefulnessFor(addr.NodeID); w != nil {
		t.Fatal("the forgotten node's wakefulness is still in the table")
	}
	if p.reg.Get(p.node.ID()) != nil {
		t.Fatal("the node is still in the registry")
	}
}
