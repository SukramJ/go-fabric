// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	ptdef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestBridgedDeviceDeclaresPowerSource: a device whose type does not list
// PowerSource declares it, as BridgedNode allows under the condition
// BridgedPowerSourceInfo (bridged-node.element.ts:23 at 862ad75f); a
// controller reads its BatPercentRemaining and an EndpointList naming the
// device's own endpoint, and the host's Set reaches the next read.
func TestBridgedDeviceDeclaresPowerSource(t *testing.T) {
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"},
		psdef.Declare{Features: psdef.FeatureBattery, Initial: psdef.Patch{
			Order: new(uint8(0)), BatReplacementNeeded: new(false), BatPercentRemaining: new(uint8(160)),
		}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(light.Spec().DeviceConditions, device.ConditionBridgedPowerSourceInfo) {
		t.Fatalf("conditions %v, want %s stated", light.Spec().DeviceConditions, device.ConditionBridgedPowerSourceInfo)
	}
	if light.PowerSource() == nil {
		t.Fatal("PowerSource() is nil for a declared PowerSource")
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{light}},
	})
	ep, ok := light.EndpointNumber()
	if !ok {
		t.Fatal("no endpoint number")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	values, err := fabrictest.ReadCluster(ctx, pair, ep, psdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := psdef.StateFrom(values)
	if state.BatPercentRemaining == nil || *state.BatPercentRemaining != 160 {
		t.Fatalf("BatPercentRemaining %v, want 160", state.BatPercentRemaining)
	}
	if !slices.Equal(state.EndpointList, []uint16{ep}) {
		t.Fatalf("EndpointList %v, want [%d]", state.EndpointList, ep)
	}
	if state.Description != "Battery power" || state.BatChargeLevel != psdef.BatChargeLevelOk {
		t.Fatalf("defaults: Description %q, BatChargeLevel %v", state.Description, state.BatChargeLevel)
	}

	desc, err := fabrictest.ReadCluster(ctx, pair, ep, descriptordef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := descriptordef.StateFrom(desc)
	if !slices.Contains(ds.ServerList, psdef.ClusterID) {
		t.Fatalf("ServerList %v lacks PowerSource", ds.ServerList)
	}

	if err := pair.Device.Act(func(*node.Node) error {
		return light.PowerSource().Set(psdef.Patch{BatPercentRemaining: new(uint8(40))})
	}); err != nil {
		t.Fatal(err)
	}
	values, err = fabrictest.ReadCluster(ctx, pair, ep, psdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	if state, _ = psdef.StateFrom(values); state.BatPercentRemaining == nil || *state.BatPercentRemaining != 40 {
		t.Fatalf("after Set: BatPercentRemaining %v, want 40", state.BatPercentRemaining)
	}
}

// TestElectricalSensorDeclaresPowerSource: the energy-store shape of the
// cookbook — an ElectricalSensor with its own measurement and a battery —
// builds, and a type that lists no PowerSource states no condition
// without the declaration.
func TestElectricalSensorDeclaresPowerSource(t *testing.T) {
	_, err := device.NewElectricalSensor(device.Options{StableKey: device.StringKey("store"), Name: "store"},
		ptdef.Declare{Features: ptdef.FeatureNodeTopology},
		epmdef.Declare{Features: epmdef.FeatureAlternatingCurrent},
		psdef.Declare{Features: psdef.FeatureBattery | psdef.FeatureRechargeable})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("plain"), Name: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(plain.Spec().DeviceConditions, device.ConditionBridgedPowerSourceInfo) {
		t.Fatal("a device without PowerSource states BridgedPowerSourceInfo")
	}
}

// TestPartRefusesBridgedPowerSource: the rule is BridgedNode's, and a part
// is no bridged node.
func TestPartRefusesBridgedPowerSource(t *testing.T) {
	owner, err := device.NewDimmableLight(device.Options{StableKey: device.StringKey("owner"), Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	part, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("part"), Name: "part"},
		psdef.Declare{Features: psdef.FeatureBattery})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(part.Device); !errors.Is(err, device.ErrComposition) {
		t.Fatalf("AddPart: %v, want ErrComposition", err)
	}
}
