// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestBaseLabelsByDeclaration: FixedLabel and UserLabel, which the Base
// device type allows on every endpoint (matter.js
// packages/model/src/standard/elements/base.element.ts:46-47 at 862ad75f),
// declared on an OnOffLight: a controller reads the fixed labels, its
// UserLabel write reaches the host's OnChanged, and the written list
// survives the device's restart.
func TestBaseLabelsByDeclaration(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	fixed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "Lamp"},
		fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &fixed}},
		userlabeldef.Declare{})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var written [][]userlabeldef.LabelStruct
	var origins []contract.Origin
	light.UserLabel().OnChanged(func(s userlabeldef.State, c contract.Change) {
		mu.Lock()
		written = append(written, s.LabelList)
		origins = append(origins, c.Origin)
		mu.Unlock()
	})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ctx := observeContext(t)
	ep, _ := light.EndpointNumber()

	values, err := fabrictest.ReadCluster(ctx, pair, ep, fixedlabeldef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := fixedlabeldef.StateFrom(values); !slices.Equal(state.LabelList, fixed) {
		t.Errorf("FixedLabel over the wire %+v, want %+v", state.LabelList, fixed)
	}

	user := spec.List[userlabeldef.LabelStruct]{{Label: "owner", Value: "me"}}
	if err := fabrictest.Write(ctx, pair, client.AttributeAt(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList), user); err != nil {
		t.Fatal(err)
	}
	// A list write may arrive as the list cleared, then each entry
	// appended: the host sees each step, the last one the list.
	mu.Lock()
	if n := len(written); n == 0 || !slices.Equal(written[n-1], []userlabeldef.LabelStruct(user)) {
		t.Errorf("OnChanged saw %+v, want the written list last", written)
	}
	for _, o := range origins {
		if o != contract.OriginControllerWrite {
			t.Errorf("OnChanged origin %v, want the controller's write", o)
		}
	}
	mu.Unlock()
}

// TestBridgedUserLabelPersisted: a bridged device's UserLabel list is kept
// in the node's store under its stable key — LabelList is nonvolatile
// (matter.js packages/model/src/standard/elements/user-label.element.ts:21-22
// at 862ad75f) — and a new node on the same store with a new device of the
// same key serves it again.
func TestBridgedUserLabelPersisted(t *testing.T) {
	t.Parallel()
	ctx := observeContext(t)
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	boot := func() (*node.Node, *device.OnOffLight) {
		t.Helper()
		n := newNode(t, minimalConfig(db))
		light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "Lamp"}, userlabeldef.Declare{})
		if err != nil {
			t.Fatal(err)
		}
		if err := n.Aggregator().Add(ctx, light); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return n, light
	}
	n, light := boot()
	want := spec.List[userlabeldef.LabelStruct]{{Label: "owner", Value: "me"}}
	if err := light.UserLabel().Underlying().MatterWrite(ctx, userlabeldef.AttrLabelList, want); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	n, light = boot()
	defer func() { _ = n.Stop(ctx) }()
	if got := light.UserLabel().Get().LabelList; !slices.Equal(got, []userlabeldef.LabelStruct(want)) {
		t.Errorf("restored %+v, want %+v", got, want)
	}
}
