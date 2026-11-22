// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
)

// The file keeps its name — the closed-findings register cites it — though
// the test in it no longer reaches into the package: the close it pins
// moved from the facade's hook into the bridge.

// TestPASEClosedOnFailSafeExpiryParity pins the fail-safe rollback's step
// 1, matter.js packages/protocol/src/common/FailsafeContext.ts:281-283 at
// b25ed960 (closePaseSession), through the facade's own root clusters: when
// the root's fail-safe expires, the node holds no PASE session — the one
// AddNOC adopted onto a fabric included — at once outside an interaction,
// after the response inside one on the PASE session (ArmFailSafe with
// expiry 0 over PASE; over CASE at once, bridge/pase_close_rollback_test.go). The close is
// the bridge's (bridge.Bridge.FailSafeExpired, wired into the root's
// GeneralCommissioning by AttachRootClusters); the facade adds nothing to
// it. bridge/pase_close_failsafe_expiry_test.go holds the same without the
// facade, on the fail-safe's timer too.
func TestPASEClosedOnFailSafeExpiryParity(t *testing.T) {
	armed := func(t *testing.T) (*node.Node, uint16) {
		t.Helper()
		n := startedNode(t, mdns.NewNoop())
		e, err := n.Sessions().OpenFromPase(1, 2, 7, []byte("pase-shared-secret"))
		if err != nil {
			t.Fatalf("OpenFromPase: %v", err)
		}
		if err := n.Sessions().AdoptFabricIndex(e.SessionID, 1); err != nil {
			t.Fatalf("AdoptFabricIndex: %v", err)
		}
		if err := n.Root().GeneralCommissioning.ArmFailSafeFor(context.Background(), 60, 1); err != nil {
			t.Fatalf("ArmFailSafeFor: %v", err)
		}
		if got := paseSessions(n); got != 1 {
			t.Fatalf("PASE sessions under the armed fail-safe = %d, want 1", got)
		}
		return n, e.SessionID
	}
	t.Run("outside an interaction", func(t *testing.T) {
		n, _ := armed(t)
		n.Root().GeneralCommissioning.ExpireFailSafe(context.Background())
		if got := paseSessions(n); got != 0 {
			t.Errorf("PASE sessions after the fail-safe expired = %d, want 0", got)
		}
	})
	t.Run("in an interaction on the PASE session", func(t *testing.T) {
		n, pase := armed(t)
		after := &im.AfterResponse{}
		n.Root().GeneralCommissioning.ExpireFailSafe(core.WithInvokeSessionID(im.WithAfterResponse(context.Background(), after), pase))
		if got := paseSessions(n); got != 1 {
			t.Fatalf("PASE sessions before the response = %d, want 1", got)
		}
		after.Run()
		if got := paseSessions(n); got != 0 {
			t.Errorf("PASE sessions after the response = %d, want 0", got)
		}
	})
}

// paseSessions counts the node's open PASE sessions.
func paseSessions(n *node.Node) int {
	count := 0
	for _, s := range n.Sessions().Sessions() {
		if s.IsPASE {
			count++
		}
	}
	return count
}
