// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// TestServerNodeOrphanedFabricDataParityMatterJS ports matter.js
// packages/node/test/node/ServerNodeTest.ts:1640-1679 at 862ad75f
// ("sanitizes orphaned fabric-scoped data at startup"): matter.js persists
// an ACL entry of a fabric that does not exist and strips it when the node
// starts (limitNodeDataToAllowedFabrics, ServerEnvironment.ts:187-195). The
// Go store cannot hold it in the first place — an area's write for a
// fabric the store does not have is refused (store.Port, storetest's
// unknown-fabric-refused) — so the port injects the entry at the store,
// sees it refused, and holds that the restarted node serves the fabric's
// own entry and none of the missing fabric's.
func TestServerNodeOrphanedFabricDataParityMatterJS(t *testing.T) {
	t.Parallel()
	s := newResetStores(t)
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: s.configure}})
	ctx := parityCtx(t)
	own := firstFabricOf(t, p)
	acl, err := s.credentials.ListACL(ctx, own)
	if err != nil || len(acl) != 1 {
		t.Fatalf("ACL %+v (%v), want the commissioning's entry", acl, err)
	}
	orphan := own + 1
	orphaned := acl[0]
	orphaned.FabricIndex = orphan
	if err := s.credentials.ReplaceACL(ctx, orphan, []store.ACLEntry{orphaned}); err == nil {
		t.Fatal("the store kept an ACL entry of a fabric it does not have")
	}
	if err := p.Device.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.credentials.ListACL(ctx, orphan); err == nil && len(got) != 0 {
		t.Fatalf("orphaned entries after the restart: %+v", got)
	}
	if got, err := s.credentials.ListACL(ctx, own); err != nil || len(got) != 1 {
		t.Fatalf("own entries after the restart %+v (%v), want 1", got, err)
	}
}

func firstFabricOf(t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	infos, err := p.Device.Node().FabricInfos(parityCtx(t))
	if err != nil || len(infos) != 1 {
		t.Fatalf("fabrics %+v (%v)", infos, err)
	}
	return infos[0].FabricIndex
}
