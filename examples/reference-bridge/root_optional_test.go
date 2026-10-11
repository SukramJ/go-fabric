// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	lcfgdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	swdiagdef "github.com/SukramJ/go-fabric/cluster/spec/softwarediagnostics"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	unitdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
)

// TestRootMountsLabelsLocalizationAndDiagnostics: the root serves the seven
// optional clusters, and a UserLabel list a controller wrote survives a
// restart on the same database.
func TestRootMountsLabelsLocalizationAndDiagnostics(t *testing.T) {
	t.Parallel()
	servers, _ := testRootClusters(t)
	ids := make([]uint32, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.MatterClusterID())
	}
	for _, id := range []uint32{
		fixedlabeldef.ClusterID, userlabeldef.ClusterID, lcfgdef.ClusterID, tfldef.ClusterID,
		unitdef.ClusterID, swdiagdef.ClusterID, ethdiagdef.ClusterID,
	} {
		if !slices.Contains(ids, id) {
			t.Errorf("root does not serve cluster 0x%04X", id)
		}
	}

	// A UserLabel list written on one boot is served on the next: two
	// nodes, one after the other, over one database.
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reference-bridge.db")
	userLabel := func(n *node.Node) contract.ClusterServer {
		t.Helper()
		for _, s := range n.Root().Servers() {
			if s.MatterClusterID() == userlabeldef.ClusterID {
				return s
			}
		}
		t.Fatal("no UserLabel")
		return nil
	}
	first := testNode(t, newFleet(), path)
	want := spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "office"}}
	if err := userLabel(first).MatterWrite(ctx, userlabeldef.AttrLabelList, want); err != nil {
		t.Fatal(err)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	second := testNode(t, newFleet(), path)
	if v, _ := userLabel(second).MatterRead(userlabeldef.AttrLabelList); !slices.Equal(v.(spec.List[userlabeldef.LabelStruct]), want) {
		t.Errorf("after restart LabelList %v, want %v", v, want)
	}
}
