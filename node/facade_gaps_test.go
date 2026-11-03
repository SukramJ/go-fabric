// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestSubscriptionSnapshot (brief M9-module §3 a): a controller's
// subscription is listed by Subscriptions().Snapshot with its paths,
// session and intervals, and its session in SessionDiagnostics carries
// its id. matter.js tests no list of its own: SessionsBehaviorTest checks
// the sessions' activity and intervals, SubscriptionsServerTest the
// persisted records across restarts — the record Snapshot copies
// (SubscriptionsServer.ts:149-159 at 862ad75f).
func TestSubscriptionSnapshot(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	ctx := parityCtx(t)
	path := lifecycleAttr(0, bidef.ClusterID, bidef.AttrNodeLabel)
	sub, err := fabrictest.Subscribe(ctx, p, fabrictest.SubscribeRequest{Attributes: []im.ConcreteAttributePath{path}, MinIntervalFloor: new(time.Second), MaxIntervalCeiling: new(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	n := p.Device.Node()
	var snap []node.SubscriptionInfo
	if err := site.Eventually(ctx, "the subscription listed", func() bool {
		snap = n.Subscriptions().Snapshot()
		return len(snap) == 1
	}); err != nil {
		t.Fatal(err)
	}
	s := snap[0]
	if s.ID != sub.ID() || s.SessionID == 0 || s.FabricIndex == 0 || s.PeerNodeID == 0 || s.MinIntervalFloor != 1 || s.MaxIntervalCeiling != 30 || s.SendInterval <= 0 {
		t.Errorf("snapshot = %+v (subscription %d)", s, sub.ID())
	}
	if len(s.AttributePaths) != 1 || s.AttributePaths[0].Cluster != bidef.ClusterID || s.AttributePaths[0].Attribute != bidef.AttrNodeLabel {
		t.Errorf("paths = %+v", s.AttributePaths)
	}
	s.AttributePaths[0].Cluster = 0
	if again := n.Subscriptions().Snapshot(); again[0].AttributePaths[0].Cluster != bidef.ClusterID {
		t.Error("the snapshot shares the subscription's paths")
	}
	var found bool
	for _, info := range n.SessionDiagnostics().Sessions {
		if info.ID == s.SessionID {
			found = true
			if info.Subscriptions != 1 || !slices.Equal(info.SubscriptionIDs, []uint32{s.ID}) {
				t.Errorf("session %d: %d subscriptions, ids %v", info.ID, info.Subscriptions, info.SubscriptionIDs)
			}
		} else if len(info.SubscriptionIDs) != 0 {
			t.Errorf("session %d carries ids %v", info.ID, info.SubscriptionIDs)
		}
	}
	if !found {
		t.Errorf("no session %d", s.SessionID)
	}
}

// TestTopologyScopeTagsLabels (brief M9-module §3 b, c): a device added to
// a named partition shows its scope and is found as Added; one in the
// default partition shows Scope; neither shows tags, as a bridged
// Descriptor serves no TagList (the tagged case is
// TestTagsAndLabelsOfServers); FixedLabel and UserLabel entries are listed
// as declared.
func TestTopologyScopeTagsLabels(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	fixed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	user := []userlabeldef.LabelStruct{{Label: "note", Value: "spare"}}
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "Lamp"},
		fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &fixed}},
		userlabeldef.Declare{Initial: userlabeldef.Patch{LabelList: &user}})
	if err != nil {
		t.Fatal(err)
	}
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "Plug"})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ctx := parityCtx(t)
	if err := p.Device.Act(func(n *node.Node) error { return n.Aggregator().Partition("ccu1").Add(ctx, plug) }); err != nil {
		t.Fatal(err)
	}
	n := p.Device.Node()
	if got := n.Aggregator().Partitions(); !slices.Equal(got, []string{node.Scope, "ccu1"}) {
		t.Errorf("Partitions = %v", got)
	}
	devices := map[string]node.BridgedDevice{}
	for _, d := range n.Topology().Devices {
		devices[d.Key.String()] = d
	}
	l, pl := devices[device.StringKey("light").String()], devices[device.StringKey("plug").String()]
	if l.Scope != node.Scope || pl.Scope != "ccu1" {
		t.Errorf("scopes %q, %q", l.Scope, pl.Scope)
	}
	if pl.Added != node.Endpoint(plug) || l.Added != node.Endpoint(light) {
		t.Errorf("Added: light %v, plug %v", l.Added, pl.Added)
	}
	if l.Tags != nil || pl.Tags != nil {
		t.Errorf("tags: light %+v, plug %+v", l.Tags, pl.Tags)
	}
	want := []node.Label{{Label: "room", Value: "hall"}, {Label: "note", Value: "spare", User: true}}
	if !slices.Equal(l.Labels, want) || pl.Labels != nil {
		t.Errorf("labels: light %+v, plug %+v", l.Labels, pl.Labels)
	}
}

// TestOnAttributeChanged (brief M9-module §3 d): a controller's write to a
// bridged endpoint reaches OnAttributeChanged once, with the endpoint,
// the cluster, the attribute and OriginControllerWrite — inline through
// Config.Observers and queued through ObserveAsync. The node observer
// tests' counterpart in matter.js is the per-behavior `$Changed` event
// (packages/node/src/behavior/Events.ts:52-56 at 862ad75f); matter.js has
// no node-wide one to port.
func TestOnAttributeChanged(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var inline, queued []node.AttributeChange
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{light},
		Configure: func(cfg *node.Config) {
			cfg.Observers.OnAttributeChanged = func(c node.AttributeChange) {
				mu.Lock()
				defer mu.Unlock()
				inline = append(inline, c)
			}
		},
	}})
	ctx := parityCtx(t)
	unsub := p.Device.Node().ObserveAsync(node.Observers{OnAttributeChanged: func(c node.AttributeChange) {
		mu.Lock()
		defer mu.Unlock()
		queued = append(queued, c)
	}})
	defer unsub()
	ep, _ := light.EndpointNumber()
	if err := fabrictest.Write(ctx, p, lifecycleAttr(ep, onoffdef.ClusterID, onoffdef.AttrOnTime), uint16Value(30)); err != nil {
		t.Fatal(err)
	}
	writes := func(list []node.AttributeChange) (out []node.AttributeChange) {
		for _, c := range list {
			if c.Endpoint == ep && c.Cluster == onoffdef.ClusterID && slices.Contains(c.Change.Attributes, onoffdef.AttrOnTime) {
				out = append(out, c)
			}
		}
		return out
	}
	if err := site.Eventually(ctx, "the write observed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(writes(inline)) > 0 && len(writes(queued)) > 0
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for name, list := range map[string][]node.AttributeChange{"inline": writes(inline), "queued": writes(queued)} {
		if len(list) != 1 || list[0].Change.Origin != contract.OriginControllerWrite {
			t.Errorf("%s: %+v, want one controller write", name, list)
		}
	}
}
