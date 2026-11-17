// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestHeldChangeAdvancesOncePerAttributeData (review II, F-db021da0-56,
// owner decision 146): a host's change held while a write message is
// applied advances its cluster once for the attribute data it arrived
// in — not at all when that attribute data changed the cluster itself, as
// the dispatcher advanced it then, and once more when only another
// attribute data of the message did. matter.js commits each attribute
// data on its own (packages/protocol/src/action/server/AttributeWriteResponse.ts:426-435,
// :444-450 at db021da0) and a commit advances the version once if it
// changed anything (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457,
// :1476-1481).
func TestHeldChangeAdvancesOncePerAttributeData(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	ep := b.Topology().FindByID(0)
	if ep == nil {
		t.Fatal("no root endpoint")
	}
	const cluster = uint32(0x0028) // BasicInformation, served on the root
	written := attributeOf(0, cluster, 0x0005)
	other := attributeOf(0, cluster, 0x0010)
	ok := []im.WriteResult{{Path: written, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: []uint32{written.Attribute}}}}
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)

	for _, tc := range []struct {
		name    string
		inWrite bool // the change arrives in the attribute data that wrote the cluster
		want    uint32
	}{
		{"in the attribute data that changed the cluster", true, 0},
		{"in another attribute data of the message", false, 1},
	} {
		scope := writeScope{endpoints: []uint16{0}}
		b.writeHolds.begin(scope)
		first := b.writeHolds.beginAttributeData()
		if tc.inWrite {
			b.writeHolds.holdPaths(ep, mgr, []im.ConcreteAttributePath{other})
		}
		b.writeHolds.noteAttributeData(first, ok)
		b.writeHolds.beginAttributeData()
		if !tc.inWrite {
			b.writeHolds.holdPaths(ep, mgr, []im.ConcreteAttributePath{other})
		}
		before := ep.ClusterDataVersion(cluster)
		b.settleWrite(scope, recordOf(ok))
		if got := ep.ClusterDataVersion(cluster) - before; got != tc.want {
			t.Errorf("%s: the held change advanced the cluster %d times, want %d", tc.name, got, tc.want)
		}
	}
}

// TestHoldOutsideAnAttributeDataFollowsTheRecord (review II, Fable's read
// of sub round 2, F56-1): a change held while no attribute data is open —
// a group write, which numbers none — is settled against the write's whole
// record, also after an earlier write numbered its attribute data: the
// record changed the cluster, so the change advances nothing more.
func TestHoldOutsideAnAttributeDataFollowsTheRecord(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	ep := b.Topology().FindByID(0)
	if ep == nil {
		t.Fatal("no root endpoint")
	}
	const cluster = uint32(0x0028)
	written := attributeOf(0, cluster, 0x0005)
	other := attributeOf(0, cluster, 0x0010)
	ok := []im.WriteResult{{Path: written, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: []uint32{written.Attribute}}}}
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	scope := writeScope{endpoints: []uint16{0}}

	// A unicast write numbers its attribute data.
	b.writeHolds.begin(scope)
	n := b.writeHolds.beginAttributeData()
	b.writeHolds.noteAttributeData(n, ok)
	b.settleWrite(scope, recordOf(ok))

	// A group write numbers none.
	b.writeHolds.begin(scope)
	b.writeHolds.holdPaths(ep, mgr, []im.ConcreteAttributePath{other})
	before := ep.ClusterDataVersion(cluster)
	b.settleWrite(scope, recordOf(ok))
	if got := ep.ClusterDataVersion(cluster) - before; got != 0 {
		t.Errorf("a change held outside any attribute data advanced the cluster %d times, want 0 (the record changed it)", got)
	}
}

// TestPhasedClustersEndWithTheirEndpointsHold (review II, Fable's read of
// sub round 2, F56-2): the clusters an attribute data changed are dropped
// once no write holds their endpoint, not only when no write is applied at
// all — writes that overlap pairwise on two endpoints keep the table
// bounded.
func TestPhasedClustersEndWithTheirEndpointsHold(t *testing.T) {
	t.Parallel()
	var tbl writeHoldTable
	scopes := [2]writeScope{{endpoints: []uint16{0}}, {endpoints: []uint16{1}}}
	tbl.begin(scopes[0])
	for i := range 2000 {
		tbl.begin(scopes[(i+1)%2]) // the next write begins before this one ends
		ep := uint16(i % 2)
		path := attributeOf(ep, 0x0006, 0x0000)
		phase := tbl.beginAttributeData()
		tbl.noteAttributeData(phase, []im.WriteResult{{Path: path, Status: im.StatusSuccess}})
		tbl.end(scopes[i%2], writeRecord{})
	}
	tbl.mu.Lock()
	n := len(tbl.changedIn)
	tbl.mu.Unlock()
	if n > 2 {
		t.Fatalf("%d attribute data's clusters kept, want at most those of the writes still applied", n)
	}
}

// TestQuieterHeldInTwoAttributeDataAdvancesOnce (review II, Fable's read
// of sub round 2, F56-3): a quieter path notified in two attribute data of
// a write, the first of which changed its cluster, is checked once, against
// the one that did not — its change advances the cluster once.
func TestQuieterHeldInTwoAttributeDataAdvancesOnce(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	epID := r.ep().ID
	scope := writeScope{endpoints: []uint16{epID}}
	written := attributeOf(epID, r.path.Cluster, epmVoltage)
	ok := []im.WriteResult{{Path: written, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: []uint32{written.Attribute}}}}
	r.b.writeHolds.begin(scope)
	first := r.b.writeHolds.beginAttributeData()
	r.src.setPower(10) // held in the attribute data that changed the cluster
	r.b.writeHolds.noteAttributeData(first, ok)
	second := r.b.writeHolds.beginAttributeData()
	r.src.setPower(20) // held again in one that did not
	r.b.writeHolds.noteAttributeData(second, nil)
	v0 := r.version()
	r.b.settleWrite(scope, recordOf(ok))
	if got := r.version() - v0; got != 1 {
		t.Fatalf("the quieter change advanced the cluster %d times, want 1", got)
	}
}
