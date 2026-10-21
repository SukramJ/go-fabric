// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The device kit's self-held defaults end to end (gate 1, G1-1, brief
// BX1): a controller commissions a bridge of a device.NewDimmableLight and
// a device.NewContactSensor built with nothing but their options — the
// gate's consumer experiment — and toggles the light, dims it, and reads
// the sensor, as a matter.js DimmableLightDevice and ContactSensorDevice
// answer: packages/node/src/devices/dimmable-light.ts:103-116 and
// packages/node/src/devices/contact-sensor.ts:68 mount OnOffServer,
// LevelControlServer and BooleanStateServer as their defaults.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func attributePath(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func TestKitDefaultsOverTheWire(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	sensor, err := device.NewContactSensor(device.Options{StableKey: endpoint.StringKey("door"), Name: "door"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{light, sensor}},
	})
	lightEP, _ := pair.Device.EndpointOf("light")
	doorEP, _ := pair.Device.EndpointOf("door")
	ctx := context.Background()

	// OnOff Toggle.
	toggle := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}
	if res, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("Toggle: %+v, %v", res, err)
	}
	if got, err := fabrictest.Read(ctx, pair, attributePath(lightEP, onoffdef.ClusterID, onoffdef.AttrOnOff)); err != nil || !got.Value.El.Bool {
		t.Fatalf("OnOff after Toggle %+v, %v", got, err)
	}

	// LevelControl MoveToLevel — UNSUPPORTED_COMMAND on the generated
	// stub before this wave.
	move := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: lvldef.ClusterID, Command: lvldef.CmdMoveToLevel}
	req := lvldef.MoveToLevelRequest{Level: 100, TransitionTime: spec.ValueOf(uint16(0))}
	if res, err := fabrictest.Invoke(ctx, pair, move, req); err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("MoveToLevel: %+v, %v", res, err)
	}
	if got, err := fabrictest.Read(ctx, pair, attributePath(lightEP, lvldef.ClusterID, lvldef.AttrCurrentLevel)); err != nil || got.Value.El.Uint != 100 {
		t.Fatalf("CurrentLevel after MoveToLevel %+v, %v", got, err)
	}
	if lvl := light.LevelControl().Get().CurrentLevel; lvl == nil || *lvl != 100 {
		t.Errorf("the host reads CurrentLevel %v", lvl)
	}

	// The host's own change through the typed server reaches the
	// controller: the light dimmed at the wall, the door opened.
	if err := pair.Device.Act(func(*node.Node) error {
		level, open := uint8(40), true
		if err := light.LevelControl().Set(lvldef.Patch{CurrentLevel: &level}); err != nil {
			return err
		}
		return sensor.BooleanState().Set(booleanstatedef.Patch{StateValue: &open})
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := fabrictest.Read(ctx, pair, attributePath(lightEP, lvldef.ClusterID, lvldef.AttrCurrentLevel)); err != nil || got.Value.El.Uint != 40 {
		t.Fatalf("CurrentLevel after the host's Set %+v, %v", got, err)
	}

	// BooleanState read.
	if got, err := fabrictest.Read(ctx, pair, attributePath(doorEP, booleanstatedef.ClusterID, booleanstatedef.AttrStateValue)); err != nil || !got.Value.El.Bool {
		t.Fatalf("StateValue %+v, %v", got, err)
	}
}
