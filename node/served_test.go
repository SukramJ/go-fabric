// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestReadServedAttributes (G-N1, issue #388): the facade reads the values
// a bridged device serves through the node's own read path, with the
// cluster's DataVersion, and renders them by name exactly as the
// controller renders the same attributes read over the wire.
func TestReadServedAttributes(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	label := spec.Nullable[string]{Value: "Left"}
	light, err := device.NewOnOffLight(device.Options{
		StableKey: device.StringKey("light"), Name: "Lamp",
		Tags: []device.SemanticTag{{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: 7, Tag: 1, Label: &label}},
	}, onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(true)}})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	n := pair.Device.Node()
	ep, _ := light.EndpointNumber()
	ctx := observeContext(t)

	v, err := n.ReadAttribute(ctx, ep, onoffdef.ClusterID, onoffdef.AttrOnOff)
	if err != nil {
		t.Fatal(err)
	}
	if v.Value != true || v.Endpoint != ep || v.Cluster != onoffdef.ClusterID || v.Attribute != onoffdef.AttrOnOff || v.DataVersion == 0 {
		t.Fatalf("OnOff = %+v", v)
	}
	values, err := n.ReadCluster(ctx, ep, descdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint32, 0, len(values))
	for _, a := range values {
		ids = append(ids, a.Attribute)
	}
	if !slices.Contains(ids, descdef.AttrTagList) || !slices.Contains(ids, 0xFFFB) || !slices.Contains(ids, 0xFFFD) {
		t.Fatalf("Descriptor attributes %v, want TagList and the globals", ids)
	}

	// The controller reads the endpoint over the wire: both sides render
	// each attribute alike, and the DataVersions agree.
	cn, err := pair.Node()
	if err != nil {
		t.Fatal(err)
	}
	err = site.Run(ctx, func(ctx context.Context) error {
		_, err := cn.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	image := cn.Endpoint(ep).Cluster(descdef.ClusterID)
	if version, ok := image.Version(); !ok || version != values[0].DataVersion {
		t.Errorf("DataVersion: the controller holds %d %v, the facade read %d", version, ok, values[0].DataVersion)
	}
	for _, a := range values {
		local, err := a.JSON()
		if err != nil {
			t.Fatal(err)
		}
		cached, ok := image.Value(a.Attribute)
		if !ok {
			t.Fatalf("0x%04X not in the controller's image", a.Attribute)
		}
		wire, err := client.DecodeJSON(client.AttributeField(descdef.ClusterID, a.Attribute), cached, client.NameBased)
		if err != nil {
			t.Fatal(err)
		}
		if string(local) != string(wire) {
			t.Errorf("0x%04X renders %s on the node, %s at the controller", a.Attribute, local, wire)
		}
	}
	tags, err := n.ReadAttribute(ctx, ep, descdef.ClusterID, descdef.AttrTagList)
	if err != nil {
		t.Fatal(err)
	}
	if js, err := tags.JSON(); err != nil || string(js) != `[{"label":"Left","mfgCode":null,"namespaceId":7,"tag":1}]` {
		t.Errorf("TagList renders %s, %v", js, err)
	}

	for _, c := range []struct {
		endpoint  uint16
		cluster   uint32
		attribute uint32
		status    im.StatusCode
	}{
		{0x7777, onoffdef.ClusterID, onoffdef.AttrOnOff, im.StatusUnsupportedEndpoint},
		{ep, basicinfo.ClusterID, 0, im.StatusUnsupportedCluster},
		{ep, onoffdef.ClusterID, 0x7777, im.StatusUnsupportedAttribute},
	} {
		_, err := n.ReadAttribute(ctx, c.endpoint, c.cluster, c.attribute)
		var se contract.StatusError
		if !errors.As(err, &se) || se.Status != uint8(c.status) {
			t.Errorf("read %d/0x%04X/0x%04X: %v, want status 0x%02X", c.endpoint, c.cluster, c.attribute, err, uint8(c.status))
		}
	}
	_, err = n.ReadCluster(ctx, ep, basicinfo.ClusterID)
	var se contract.StatusError
	if !errors.As(err, &se) || se.Status != uint8(im.StatusUnsupportedCluster) {
		t.Errorf("ReadCluster of an unserved cluster: %v", err)
	}
}

// TestReadServedBeforeStart: a node not started has no read path.
func TestReadServedBeforeStart(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if _, err := n.ReadAttribute(context.Background(), 0, basicinfo.ClusterID, basicinfo.AttrVendorName); !errors.Is(err, node.ErrNotStarted) {
		t.Fatalf("ReadAttribute before Start: %v", err)
	}
	if _, err := n.ReadCluster(context.Background(), 0, basicinfo.ClusterID); !errors.Is(err, node.ErrNotStarted) {
		t.Fatalf("ReadCluster before Start: %v", err)
	}
	if e := n.Events(node.EventQuery{}); len(e) != 0 {
		t.Fatalf("events before Start: %+v", e)
	}
}

// TestServedEvents (G-N3, issue #388): the facade queries the node's event
// log and OnEvent reports each new event with its record — the Leave event
// a host's RemoveFabric emits among them.
func TestServedEvents(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{})
	n := pair.Device.Node()
	ctx := observeContext(t)

	startUp := n.Events(node.EventQuery{Endpoint: new(uint16(0)), Cluster: new(basicinfo.ClusterID), Event: new(basicinfo.EventStartUp)})
	if len(startUp) != 1 || startUp[0].Priority != contract.EventPriorityCritical || startUp[0].Time.IsZero() {
		t.Fatalf("StartUp events %+v", startUp)
	}
	js, err := startUp[0].JSON()
	if err != nil || string(js) == "null" {
		t.Fatalf("StartUp renders %s, %v", js, err)
	}
	all := n.Events(node.EventQuery{})
	if len(all) == 0 || !slices.IsSortedFunc(all, func(a, b node.Event) int { return int(a.Number) - int(b.Number) }) {
		t.Fatalf("events %+v, want ascending numbers", all)
	}
	if last := n.Events(node.EventQuery{Limit: 1}); len(last) != 1 || last[0].Number != all[len(all)-1].Number {
		t.Fatalf("Limit 1 = %+v, want the newest", last)
	}
	if none := n.Events(node.EventQuery{MinNumber: all[len(all)-1].Number + 1}); len(none) != 0 {
		t.Fatalf("MinNumber past the newest = %+v", none)
	}

	var mu sync.Mutex
	var seen []node.Event
	unsubscribe := n.Observe(node.Observers{OnEvent: func(e node.Event) {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	}})
	defer unsubscribe()
	fabrics, err := n.FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %+v", fabrics)
	}
	if err := n.RemoveFabric(ctx, fabrics[0].FabricIndex); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	i := slices.IndexFunc(seen, func(e node.Event) bool {
		return e.Cluster == basicinfo.ClusterID && e.Event == basicinfo.EventLeave
	})
	if i < 0 {
		t.Fatalf("OnEvent saw %+v, want the Leave event", seen)
	}
	logged := n.Events(node.EventQuery{MinNumber: seen[i].Number, Limit: 1, Cluster: new(basicinfo.ClusterID)})
	if len(logged) != 1 || logged[0].Number != seen[i].Number || logged[0].Event != seen[i].Event {
		t.Fatalf("the log holds %+v, OnEvent saw %+v", logged, seen[i])
	}
}

// listedServer is a fakeServer that lists attributes in its AttributeList
// beyond the ones it reads — an attribute whose read fails.
type listedServer struct {
	*fakeServer
	listed []uint32
}

func (l *listedServer) MatterAttributes() []uint32 { return l.listed }

// TestReadClusterReportsFailedAttributes (issue #396): ReadCluster returns
// every attribute the wildcard read reports, in its order, a failed one
// with its status and no value — the per-path status a controller's
// wildcard read of the same cluster receives — while ReadAttribute keeps
// returning the failure as its error.
func TestReadClusterReportsFailedAttributes(t *testing.T) {
	t.Parallel()
	const cluster, readable, unreadable = 0xFFF1FC01, 0x0000, 0x0001
	srv := &listedServer{
		fakeServer: &fakeServer{cluster: cluster, attrs: map[uint32]any{readable: uint8(7)}},
		listed:     []uint32{readable, unreadable},
	}
	ep := &customEndpoint{key: "listed", deviceType: chimeDeviceType, servers: []contract.ClusterServer{srv}}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{ep}}})
	n := pair.Device.Node()
	num, ok := pair.Device.EndpointOf("listed")
	if !ok {
		t.Fatal("no endpoint")
	}
	ctx := observeContext(t)
	values, err := n.ReadCluster(ctx, num, cluster)
	if err != nil {
		t.Fatal(err)
	}
	var failed, served *node.AttributeValue
	for i := range values {
		switch values[i].Attribute {
		case unreadable:
			failed = &values[i]
		case readable:
			served = &values[i]
		}
	}
	if served == nil || served.Status != 0 || served.Value != uint8(7) {
		t.Fatalf("readable attribute %+v", served)
	}
	if failed == nil || failed.Status != uint8(im.StatusUnsupportedAttribute) || failed.Value != nil {
		t.Fatalf("unreadable attribute %+v, want status UNSUPPORTED_ATTRIBUTE without a value", failed)
	}
	if _, err := n.ReadAttribute(ctx, num, cluster, unreadable); err == nil {
		t.Fatal("ReadAttribute of the unreadable attribute succeeded")
	}

	// The controller's wildcard read of the cluster reports the same
	// paths: the values in the same order, the failed one with the same
	// status.
	var rep *fabrictest.Report
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		rep, err = fabrictest.ReadWith(ctx, pair, fabrictest.ReadRequest{Attributes: []fabrictest.AttributePath{client.ClusterAt(num, cluster)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wireValues := make([]uint32, 0, len(rep.Attributes))
	facadeValues := make([]uint32, 0, len(values))
	for _, a := range rep.Attributes {
		wireValues = append(wireValues, a.Path.Attribute)
	}
	wireStatus := map[uint32]uint8{}
	for _, st := range rep.Statuses {
		wireStatus[st.Path.Attribute] = uint8(st.Status)
	}
	facadeStatus := map[uint32]uint8{}
	for _, v := range values {
		if v.Status == 0 {
			facadeValues = append(facadeValues, v.Attribute)
		} else {
			facadeStatus[v.Attribute] = v.Status
		}
	}
	if !slices.Equal(facadeValues, wireValues) || !maps.Equal(facadeStatus, wireStatus) {
		t.Fatalf("facade values %v statuses %v, wire values %v statuses %v", facadeValues, facadeStatus, wireValues, wireStatus)
	}
}
