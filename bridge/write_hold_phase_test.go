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
