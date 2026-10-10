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
	"github.com/SukramJ/go-fabric/store"
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

	ctx := context.Background()
	db, err := openDB(ctx, filepath.Join(t.TempDir(), "reference-bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := store.New(db)
	userLabel := func() contract.ClusterServer {
		t.Helper()
		srvs, err := buildRootLabelsAndDiagnostics(ctx, st)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range srvs {
			if s.MatterClusterID() == userlabeldef.ClusterID {
				return s
			}
		}
		t.Fatal("no UserLabel")
		return nil
	}
	want := spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "office"}}
	if err := userLabel().MatterWrite(ctx, userlabeldef.AttrLabelList, want); err != nil {
		t.Fatal(err)
	}
	if v, _ := userLabel().MatterRead(userlabeldef.AttrLabelList); !slices.Equal(v.(spec.List[userlabeldef.LabelStruct]), want) {
		t.Errorf("after restart LabelList %v, want %v", v, want)
	}

	if used, ok := (runtimeHeap{}).CurrentHeapUsed(); !ok || used == 0 {
		t.Errorf("heap used %d", used)
	}
	if _, ok := (runtimeHeap{}).CurrentHeapFree(); !ok {
		t.Error("heap free unknown")
	}
	e := unknownEthernet{}
	if _, ok := e.TimeSinceReset(); ok {
		t.Error("TimeSinceReset known")
	}
}
