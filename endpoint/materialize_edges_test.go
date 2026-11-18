// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// The edges of the bridged endpoint's materialisation that no other test
// reaches (R2-P5-tmut-18 rest).

func bridgedInfoOf(t *testing.T, ep *Endpoint) *mattercore.BridgedDeviceBasicInformation {
	t.Helper()
	for _, s := range ClusterServers(ep) {
		if b, ok := s.(*mattercore.BridgedDeviceBasicInformation); ok {
			return b
		}
	}
	t.Fatal("no BridgedDeviceBasicInformation server")
	return nil
}

func descriptorOf(t *testing.T, ep *Endpoint) *mattercore.Descriptor {
	t.Helper()
	for _, s := range ClusterServers(ep) {
		if d, ok := s.(*mattercore.Descriptor); ok {
			return d
		}
	}
	t.Fatal("no Descriptor server")
	return nil
}

// TestBridgedBasicInformationVendorAndProductIDs: the bridged endpoint
// serves the node's VendorID and ProductID the assembler copied onto it,
// and the CSA test pair 0xFFF1 / 0x8001 when none was copied.
func TestBridgedBasicInformationVendorAndProductIDs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		vendor, product    uint16
		wantVendor, wantPr uint16
	}{
		{"none copied", 0, 0, 0xFFF1, 0x8001},
		{"copied", 0x1234, 0x5678, 0x1234, 0x5678},
	} {
		ep := &Endpoint{ID: 3, DeviceType: 0x0100, SourceKey: StringKey("dev"), Source: deviceTypeSource{dt: 0x0100}, BridgeVendorID: tc.vendor, BridgeProductID: tc.product}
		b := bridgedInfoOf(t, ep)
		if v, _ := b.MatterRead(0x0002); v != tc.wantVendor { // VendorID
			t.Errorf("%s: VendorID = %v, want %#x", tc.name, v, tc.wantVendor)
		}
		if v, _ := b.MatterRead(0x0004); v != tc.wantPr { // ProductID
			t.Errorf("%s: ProductID = %v, want %#x", tc.name, v, tc.wantPr)
		}
	}
}

// TestDescriptorTagListOnlyWithTags: an endpoint without tags leaves the
// Descriptor's TagList feature off; one with tags turns it on.
func TestDescriptorTagListOnlyWithTags(t *testing.T) {
	t.Parallel()
	const tagListBit = uint32(1)
	untagged := &Endpoint{ID: 3, DeviceType: 0x0100, SourceKey: StringKey("dev"), Source: deviceTypeSource{dt: 0x0100}}
	if v, _ := descriptorOf(t, untagged).MatterRead(0xFFFC); v.(uint32)&tagListBit != 0 {
		t.Errorf("FeatureMap of an untagged endpoint = %v, want TagList off", v)
	}
	tagged := &Endpoint{ID: 3, DeviceType: 0x0100, SourceKey: StringKey("dev"), Source: deviceTypeSource{dt: 0x0100}, Tags: []mattercore.SemanticTag{{NamespaceId: 7, Tag: 1}}}
	if v, _ := descriptorOf(t, tagged).MatterRead(0xFFFC); v.(uint32)&tagListBit == 0 {
		t.Errorf("FeatureMap of a tagged endpoint = %v, want TagList on", v)
	}
}

// TestServerListNamesEachClusterOnce: a source that supplies a cluster the
// stack mounts as well (here Identify) is listed once in the ServerList.
func TestServerListNamesEachClusterOnce(t *testing.T) {
	t.Parallel()
	ep := &Endpoint{ID: 3, DeviceType: 0x0100, SourceKey: StringKey("dev"), Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{
		fakeServer{id: 0x0003}, fakeServer{id: 0x0006},
	}}}
	v, _ := descriptorOf(t, ep).MatterRead(0x0001) // ServerList
	ids, _ := v.([]uint32)
	seen := map[uint32]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("ServerList %v names 0x%04X twice", ids, id)
		}
		seen[id] = true
	}
	if !seen[0x0003] || !seen[0x0006] {
		t.Fatalf("ServerList %v, want Identify and OnOff", ids)
	}
}

// TestRenderSourceKeyEmptyShapes: a key whose reflective rendering carries
// nothing renders as "", the empty identity.
func TestRenderSourceKeyEmptyShapes(t *testing.T) {
	t.Parallel()
	type empty struct{}
	if got := renderSourceKey(empty{}); got != "" {
		t.Errorf("renderSourceKey(struct{}{}) = %q, want \"\"", got)
	}
}

// TestCommandOnANonSceneClusterKeepsTheScene: only an OnOff, LevelControl
// or ColorControl command ends the endpoint's current scene; a command on
// another cluster leaves it valid.
func TestCommandOnANonSceneClusterKeepsTheScene(t *testing.T) {
	t.Parallel()
	var invoked []uint16
	mgr, _ := groups.NewManager(oneFabricGroupStore{}, nil)
	ep := &Endpoint{
		ID: 7, DeviceType: 0x0100, FriendlyName: "Lamp", SourceKey: StringKey("lamp"),
		Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{
			commandServer{fakeServer: fakeServer{id: 0x0102}, accepted: []uint32{0}, invoked: &invoked, ep: 7},
		}},
		groups: mgr,
	}
	var scenes *mattercore.ScenesManagement
	for _, s := range ClusterServers(ep) {
		if sm, ok := s.(*mattercore.ScenesManagement); ok {
			scenes = sm
		}
	}
	if scenes == nil {
		t.Fatal("no stack ScenesManagement server")
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	if _, err := scenes.MatterInvoke(ctx, 0x04, mattercore.SceneRef{GroupID: 0, SceneID: 1}); err != nil {
		t.Fatal(err)
	}
	valid := func() bool {
		v, _ := scenes.MatterReadFiltered(ctx, 0x0002)
		si, _ := v.([]mattercore.SceneInfoStruct)
		return len(si) == 1 && si[0].SceneValid
	}
	if !valid() {
		t.Fatal("the stored scene is not current")
	}
	d := NewTopologyDispatcher(makeTopology(ep))
	path := im.ConcreteCommandPath{Endpoint: 7, HasEndpoint: true, Cluster: 0x0102, HasCluster: true, Command: 0x00, HasCommand: true}
	if res := d.InvokeAuthorized(ctx, path, nil, nil); len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("results %+v", res)
	}
	if !valid() {
		t.Error("a WindowCovering command ended the current scene")
	}
}

// groupMappedStore is oneFabricGroupStore with group 0x0005 of fabric 1
// mapped to key set 1.
type groupMappedStore struct{ oneFabricGroupStore }

func (groupMappedStore) ListGroupKeyMappings(_ context.Context, fabric uint8) ([]store.GroupKeyMapping, error) {
	if fabric != 1 {
		return nil, nil
	}
	return []store.GroupKeyMapping{{FabricIndex: 1, GroupID: 0x0005, GroupKeySetID: 1}}, nil
}

// TestScenesOfAGroupNeedTheGroupKeyMapping: the stack's ScenesManagement
// knows a group by the fabric's GroupKeyMap — a scene of a mapped group is
// added, one of an unmapped group refused.
func TestScenesOfAGroupNeedTheGroupKeyMapping(t *testing.T) {
	t.Parallel()
	mgr, err := groups.NewManager(groupMappedStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ep := &Endpoint{
		ID: 7, DeviceType: 0x0100, FriendlyName: "Lamp", SourceKey: StringKey("lamp"),
		Source: deviceTypeSource{dt: 0x0100},
		groups: mgr,
	}
	var scenes *mattercore.ScenesManagement
	for _, s := range ClusterServers(ep) {
		if sm, ok := s.(*mattercore.ScenesManagement); ok {
			scenes = sm
		}
	}
	if scenes == nil {
		t.Fatal("no stack ScenesManagement server")
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	for _, tc := range []struct {
		group uint16
		want  im.StatusCode
	}{{0x0005, im.StatusSuccess}, {0x0006, im.StatusInvalidCommand}} {
		r, err := scenes.MatterInvoke(ctx, 0x00, mattercore.AddSceneRequest{GroupID: tc.group, SceneID: 1})
		if err != nil {
			t.Fatalf("AddScene for group %#x: %v", tc.group, err)
		}
		if got := r.(mattercore.SceneStatusResponse).Status; got != tc.want {
			t.Errorf("AddScene for group %#x: status %v, want %v", tc.group, got, tc.want)
		}
	}
}
