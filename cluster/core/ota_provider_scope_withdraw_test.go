// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"

	provdef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestOTAProviderDropWithdrawsTheScopeOfAnEndedAttempt (R3-conc-3103): a
// requestor's answered attempt that ended for a reason other than the
// fabric's drop — here evicted by the per-fabric share, when sixteen other
// requestors of the fabric query — has left the attempt set; the fabric's
// drop still withdraws the BDX scope its query enabled, so a later fabric
// on the index with the same node id fetches nothing (owner decision 167).
func TestOTAProviderDropWithdrawsTheScopeOfAnEndedAttempt(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query = %+v, want UpdateAvailable", r)
	}
	for n := uint64(1); n <= otaAttemptsPerFabric; n++ {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, otaTestFabric), otaTestPeerNode+n, nil)
		if _, err := f.p.MatterInvoke(WithInvokeSessionID(ctx, uint16(100+n)), provdef.CmdQueryImage, bdxQuery()); err != nil {
			t.Fatalf("query of node %d: %v", otaTestPeerNode+n, err)
		}
	}
	m := f.p.mgr.(*otaStandaloneManager)
	m.mu.Lock()
	for _, a := range m.set.All() {
		if a.peer == otaTestPeer {
			m.mu.Unlock()
			t.Fatal("the first requestor's attempt was not evicted: the test does not reach the path")
		}
	}
	m.mu.Unlock()
	if err := f.p.MatterDropFabric(context.Background(), otaTestFabric); err != nil {
		t.Fatal(err)
	}
	if f.otaScopeEnabled(t, 9) {
		t.Error("the evicted requestor's ota scope is still enabled after its fabric's drop")
	}
}
