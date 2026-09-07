// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestBridgedEndpointsMeetTheirDeviceTypeConformance holds every bridged
// endpoint of the fleet against the device type it advertises.
//
// Two checks, from two sources. The mandatory server-cluster set is read
// from the matter.js schema snapshot the module embeds (parity/schema.json,
// requirement conformance "M"), so a regeneration moves the bar without
// an edit here. The snapshot does not carry feature conformance, so the
// one feature requirement a bridge of lights meets is pinned from the
// element file itself: OnOffLight and OnOffPlugInUnit require the OnOff
// LIGHTING feature (matter.js
// packages/model/src/standard/elements/on-off-light.element.ts:24-25,
// on-off-plug-in-unit.element.ts:24-25), and with it the LT attributes
// and commands (on-off.element.ts:30-36, :41-51).
//
// The example is what a host copies. An endpoint that advertises a device
// type and serves less than that type mandates is the drift the onoff
// package documents as the shape a controller can end a commissioning
// over — and a copied one ships it.
func TestBridgedEndpointsMeetTheirDeviceTypeConformance(t *testing.T) {
	_, br := startFleetBridge(t)
	topology := br.Topology()
	if topology == nil {
		t.Fatal("bridge has no topology after Start")
	}
	mandatory := mandatoryServerClusters(t)

	checked := 0
	for _, ep := range topology.Endpoints {
		if ep == nil || ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		checked++
		servers := endpoint.ClusterServers(ep)
		mounted := make([]uint32, 0, len(servers))
		for _, srv := range servers {
			mounted = append(mounted, srv.MatterClusterID())
		}
		deviceType := uint32(ep.DeviceType)
		name, _ := schema.DeviceTypeName(deviceType)
		want, known := mandatory[deviceType]
		if !known {
			t.Errorf("%s (endpoint %d) advertises device type %#06x, which the schema snapshot does not know", ep.DeviceAddress, ep.ID, deviceType)
			continue
		}
		for _, id := range want {
			if !slices.Contains(mounted, id) {
				clusterName, _ := schema.ClusterName(id)
				t.Errorf("%s (endpoint %d, %s %#06x) does not mount %s (%#06x), which the device type marks mandatory",
					ep.DeviceAddress, ep.ID, name, deviceType, clusterName, id)
			}
		}
		if deviceType == uint32(onoff.DeviceTypeOnOffLight) || deviceType == uint32(onoff.DeviceTypeOnOffPlugInUnit) {
			assertOnOffLighting(t, ep, servers)
		}
	}
	if checked == 0 {
		t.Fatal("the fleet mounted no bridged endpoint — nothing was checked")
	}
}

// assertOnOffLighting requires the OnOff server on ep to advertise the LT
// feature and to list the attributes and commands LT makes mandatory.
func assertOnOffLighting(t *testing.T, ep *endpoint.Endpoint, servers []contract.ClusterServer) {
	t.Helper()
	for _, srv := range servers {
		if srv.MatterClusterID() != onoff.ClusterID {
			continue
		}
		raw, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
		fm, isU32 := raw.(uint32)
		if !ok || !isU32 || fm&onoff.FeatureLighting == 0 {
			t.Errorf("%s (endpoint %d): OnOff FeatureMap = %v, want the LT bit (%#x) set — the device type mandates the LIGHTING feature",
				ep.DeviceAddress, ep.ID, raw, onoff.FeatureLighting)
		}
		lister, ok := srv.(contract.ClusterAttributeLister)
		if !ok {
			t.Errorf("%s (endpoint %d): OnOff server does not enumerate its attributes", ep.DeviceAddress, ep.ID)
		} else {
			for _, id := range onoff.LightingAttributes() {
				if !slices.Contains(lister.MatterAttributes(), id) {
					t.Errorf("%s (endpoint %d): OnOff attribute %#06x is missing from the attribute list (LT)", ep.DeviceAddress, ep.ID, id)
				}
			}
		}
		cmds, ok := srv.(contract.ClusterCommandLister)
		if !ok {
			t.Errorf("%s (endpoint %d): OnOff server does not enumerate its commands", ep.DeviceAddress, ep.ID)
		} else {
			for _, id := range onoff.LightingCommands() {
				if !slices.Contains(cmds.MatterAcceptedCommands(), id) {
					t.Errorf("%s (endpoint %d): OnOff command %#04x is missing from AcceptedCommandList (LT)", ep.DeviceAddress, ep.ID, id)
				}
			}
		}
		return
	}
	t.Errorf("%s (endpoint %d) mounts no OnOff server", ep.DeviceAddress, ep.ID)
}

// mandatoryServerClusters decodes, per device type, the server clusters the
// matter.js schema snapshot marks conformance "M".
func mandatoryServerClusters(t *testing.T) map[uint32][]uint32 {
	t.Helper()
	var snapshot struct {
		DeviceTypes []struct {
			ID           uint32 `json:"id"`
			Requirements []struct {
				ID          uint32 `json:"id"`
				Element     string `json:"element"`
				Conformance string `json:"conformance"`
			} `json:"requirements"`
		} `json:"deviceTypes"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snapshot); err != nil {
		t.Fatalf("decode schema snapshot: %v", err)
	}
	out := make(map[uint32][]uint32, len(snapshot.DeviceTypes))
	for _, dt := range snapshot.DeviceTypes {
		ids := []uint32{}
		for _, req := range dt.Requirements {
			if req.Element == "serverCluster" && req.Conformance == "M" {
				ids = append(ids, req.ID)
			}
		}
		out[dt.ID] = ids
	}
	return out
}

// TestOnWithTimedOffCountsDownAndTurnsTheLightOff drives the one LT command
// with behaviour of its own through the server the light mounts:
// OnWithTimedOff(OnTime=3) turns the light on and, three tenths of a second
// later, off again with OnTime back at 0 — matter.js OnOffServer.ts
// onWithTimedOff / #timedOnTick.
func TestOnWithTimedOffCountsDownAndTurnsTheLightOff(t *testing.T) {
	t.Parallel()
	light := newDemoLight("timed")
	var srv *onOffServer
	for _, s := range light.MatterClusterServers() {
		if o, ok := s.(*onOffServer); ok {
			srv = o
		}
	}
	if srv == nil {
		t.Fatal("the light mounts no onOffServer")
	}
	if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOnWithTimedOff,
		map[uint8]any{0: uint8(0), 1: uint16(3), 2: uint16(0)}); err != nil {
		t.Fatalf("OnWithTimedOff: %v", err)
	}
	if !light.isOn() {
		t.Fatal("OnWithTimedOff left the light off")
	}
	if v, _ := srv.MatterRead(onoff.AttrOnTime); v != uint16(3) {
		t.Fatalf("OnTime right after OnWithTimedOff = %v, want 3", v)
	}
	deadline := time.Now().Add(3 * time.Second)
	for light.isOn() {
		if time.Now().After(deadline) {
			t.Fatal("the light is still on 3 s after OnWithTimedOff(OnTime=3 tenths)")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v, _ := srv.MatterRead(onoff.AttrOnTime); v != uint16(0) {
		t.Errorf("OnTime after the countdown = %v, want 0", v)
	}
}
