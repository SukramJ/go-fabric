// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/cluster/spec"
	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	userlabeldef "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// TestUnchangedWriteKeepsTheDataVersionParity: on a bridged endpoint, whose
// cluster versions the endpoint keeps, a write that leaves the cluster as
// it was advances no DataVersion and is recorded as unchanged for the
// report; a write that changes a value advances it once. matter.js
// advances a datasource's version only for a commit that changed a value
// (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
// db021da0) and holds an equal value — a list deep-equal to the one held
// included — as no change (:1479). A list ADD always changes the list. A
// host server that does not say whether a write changed anything
// (contract.ChangeReportingWriter) advances the version with every
// successful write, as before.
func TestUnchangedWriteKeepsTheDataVersionParity(t *testing.T) {
	t.Parallel()
	light, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
	if err != nil {
		t.Fatal(err)
	}
	labels, err := mattercore.NewUserLabel(mattercore.UserLabelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	bridged, err := mattercore.NewBridgedDeviceBasicInformation(mattercore.BridgedConfig{UniqueID: "u-1", NodeLabel: "lamp"})
	if err != nil {
		t.Fatal(err)
	}
	host := onTimeServer()
	host.id = 0xFFF1FC01 // a manufacturer cluster beside OnOff
	d := gateDispatcher(t, store.PrivilegeAdminister, light, labels, bridged, host)

	label := func(l string) spec.List[userlabeldef.LabelStruct] {
		return spec.List[userlabeldef.LabelStruct]{{Label: l, Value: "v"}}
	}
	add := func(p im.ConcreteAttributePath) im.ConcreteAttributePath {
		p.HasListIndex, p.ListAppend = true, true
		return p
	}
	onTime := concreteAttrPath(2, onoff.ClusterID, onoff.AttrOnTime)
	labelList := concreteAttrPath(2, userlabeldef.ClusterID, userlabeldef.AttrLabelList)
	nodeLabel := concreteAttrPath(2, bdbidef.ClusterID, bdbidef.AttrNodeLabel)
	hostOnTime := concreteAttrPath(2, host.id, gateOnTime)
	one := func(p im.ConcreteAttributePath, v any) []im.AttributeWrite { return []im.AttributeWrite{writeOf(p, v)} }
	steps := []struct {
		name    string
		writes  []im.AttributeWrite
		changed bool
	}{
		{"OnTime 7 (generated, wrapped)", one(onTime, uint16(7)), true},
		{"OnTime 7 again", one(onTime, uint16(7)), false},
		{"OnTime 8", one(onTime, uint16(8)), true},
		{"LabelList [a] (generated list)", one(labelList, label("a")), true},
		{"LabelList [a] again", one(labelList, label("a")), false},
		// A chunked list write: the REPLACE of the list held changes
		// nothing, the ADD always changes the list.
		{"LabelList [a] again, then ADD b", []im.AttributeWrite{writeOf(labelList, label("a")), writeOf(add(labelList), label("b"))}, true},
		{"NodeLabel lamp (hand-written), as it was", one(nodeLabel, "lamp"), false},
		{"NodeLabel desk", one(nodeLabel, "desk"), true},
		{"host server without the capability, value as it was", one(hostOnTime, uint16(0)), true},
	}
	for _, s := range steps {
		path := s.writes[0].Path
		before, ok := d.CurrentDataVersion(context.Background(), 2, path.Cluster)
		if !ok {
			t.Fatalf("%s: no DataVersion", s.name)
		}
		changes := &WriteChanges{}
		ctx := WithWriteChanges(caseCtx(false), changes) //nolint:staticcheck // the deprecated record keeps its behaviour
		out := im.HandleWrite(ctx, d, im.WriteRequest{Writes: s.writes})
		resp := out.Response
		if len(resp.Responses) != len(s.writes) {
			t.Fatalf("%s: %+v", s.name, resp.Responses)
		}
		for _, r := range resp.Responses {
			if r.Status.Status != im.StatusSuccess {
				t.Fatalf("%s: %+v", s.name, resp.Responses)
			}
		}
		after, _ := d.CurrentDataVersion(context.Background(), 2, path.Cluster)
		if moved := after != before; moved != s.changed {
			t.Errorf("%s: DataVersion %d → %d, want advanced %v", s.name, before, after, s.changed)
		}
		if unchanged := changes.Unchanged(path); unchanged == s.changed {
			t.Errorf("%s: recorded unchanged %v, want %v", s.name, unchanged, !s.changed)
		}
		// The verdict in the results (im.WriteResult.Commit): every
		// successful write carries one, naming the attribute only when it
		// changed.
		named := false
		for _, r := range out.Results {
			if r.Commit == nil {
				t.Fatalf("%s: a successful write without a Commit: %+v", s.name, r)
			}
			for _, id := range r.Commit.Changed {
				named = named || id == path.Attribute
			}
		}
		if named != s.changed {
			t.Errorf("%s: Commit names the written attribute %v, want %v", s.name, named, s.changed)
		}
	}
}

// TestWriteChangesChangedWins: within one interaction a path written once
// unchanged and once changed — a chunked list's REPLACE of the held list
// and its ADD — counts as changed, and a path the dispatcher never recorded
// is not unchanged.
func TestWriteChangesChangedWins(t *testing.T) {
	t.Parallel()
	w := &WriteChanges{}
	p := concreteAttrPath(2, 0x0041, 0x0000)
	w.record(p, false)
	if !w.Unchanged(p) {
		t.Fatal("a path written once without a change is not unchanged")
	}
	w.record(p, true)
	w.record(p, false)
	if w.Unchanged(p) {
		t.Fatal("a path changed once in the interaction is unchanged")
	}
	if w.Unchanged(concreteAttrPath(2, 0x0041, 0x0001)) {
		t.Fatal("a path never recorded is unchanged")
	}
	var none *WriteChanges
	if none.Unchanged(p) {
		t.Fatal("a nil record holds a path unchanged")
	}
}

// TestWriteChangesChangedAttributes: Changed and ChangedAttributes name
// exactly the paths some write of the interaction changed — not one
// written without a change, not one never written.
func TestWriteChangesChangedAttributes(t *testing.T) {
	t.Parallel()
	w := &WriteChanges{}
	changed := concreteAttrPath(2, 0x0006, 0x4001)
	unchanged := concreteAttrPath(2, 0x0006, 0x4002)
	w.record(changed, true)
	w.record(unchanged, false)
	if !w.Changed(changed) || w.Changed(unchanged) || w.Changed(concreteAttrPath(2, 0x0006, 0x4003)) {
		t.Fatal("Changed names a path no write changed, or misses the one a write changed")
	}
	if got := w.ChangedAttributes(); len(got) != 1 || got[0] != changed {
		t.Fatalf("ChangedAttributes = %+v, want exactly %+v", got, changed)
	}
	var none *WriteChanges
	if none.Changed(changed) || none.ChangedAttributes() != nil {
		t.Fatal("a nil record names a changed path")
	}
}
