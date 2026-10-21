// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts
// at 862ad75f: an OnOff boolean scene value added and recalled over the
// wire (ScenesManagementServer.ts addScene :276-309, recallScene :434-455,
// OnOffServer's implementScenes).
//
// matter.js commissions a MockSite pair whose device is the default
// OnOffLight on endpoint 1, switches it off, adds scene 1 (1 s transition)
// and scene 2 (the 60 000 000 ms transition-time maximum) on group 0,
// recalls scene 1, advances 1.5 s and reads OnOff. The port commissions a
// fabrictest pair whose device serves a bridged OnOffLight (the device
// layer mounts ScenesManagement on it), runs the same commands over the
// wire, advances the site's clock by 1.5 s and reads OnOff. The local
// server case is TestScenesAddAndRecallOnOff (scenes_management_test.go).
package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	scenesdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestScenesManagementServerAddAndRecallOnOff ports "add and recall onoff
// boolean scene value" (ScenesManagementServerTest.ts:20-98).
func TestScenesManagementServerAddAndRecallOnOff(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, ok := pair.Device.EndpointOf("light")
	if !ok {
		t.Fatal("no light endpoint")
	}
	ctx := context.Background()
	scenes := func(cmd uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: ep, Cluster: scenesdef.ClusterID, Command: cmd}
	}

	// Ensure off.
	if _, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdOff}, nil); err != nil {
		t.Fatalf("Off: %v", err)
	}

	on := uint8(1)
	for _, s := range []struct {
		id         uint8
		transition uint32
		name       string
	}{{1, 1000, "Scene1"}, {2, 60_000_000, "Scene2"}} {
		res, err := fabrictest.Invoke(ctx, pair, scenes(scenesdef.CmdAddScene), scenesdef.AddSceneRequest{
			GroupId: 0, SceneId: s.id, TransitionTime: s.transition, SceneName: s.name,
			ExtensionFieldSetStructs: []scenesdef.ExtensionFieldSetStruct{{
				ClusterId:          onoff.ClusterID,
				AttributeValueList: []scenesdef.AttributeValuePairStruct{{AttributeId: onoff.AttrOnOff, ValueUnsigned8: &on}},
			}},
		})
		if err != nil {
			t.Fatalf("AddScene %d: %v", s.id, err)
		}
		var resp scenesdef.AddSceneResponse
		if err := resp.DecodeTLV(res.Fields); err != nil {
			t.Fatalf("AddSceneResponse %d: %v", s.id, err)
		}
		if want := (scenesdef.AddSceneResponse{Status: 0, GroupId: 0, SceneId: s.id}); resp != want {
			t.Errorf("AddScene %d = %+v, want %+v", s.id, resp, want)
		}
	}

	if _, err := fabrictest.Invoke(ctx, pair, scenes(scenesdef.CmdRecallScene), scenesdef.RecallSceneRequest{GroupId: 0, SceneId: 1}); err != nil {
		t.Fatalf("RecallScene: %v", err)
	}
	site.Fake().Advance(1500 * time.Millisecond)

	got, err := fabrictest.Read(ctx, pair, im.ConcreteAttributePath{
		Endpoint: ep, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if err != nil {
		t.Fatalf("read OnOff: %v", err)
	}
	if !got.Value.El.Bool {
		t.Error("OnOff = false after recalling scene 1, want true")
	}
}
