// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	tmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// bridgeSimCommands are the five backchannel names
// (packages/testing/src/device/backchannel.ts:143-150 at 862ad75f).
var bridgeSimCommands = []string{
	"toggleBridgedLights", "warmBridgedTemperatureSensors", "renameBridgedLights", "addBridgedLight", "removeBridgedLight",
}

// TestBridgeSimFlag: with --bridge-sim the node bridges matter.js's bridge
// test instance's devices after every other device
// (BridgeTestInstance.ts:148-172 at 862ad75f) — free of device-type
// violations but the TagList the device kit cannot declare; the composed
// device lists Power Source beside Bridged Node, its sensors are its own
// PartsList and carry no BridgedDeviceBasicInformation, its PowerSource
// names the three endpoints; each app-pipe command moves the state it
// names (:205-261), add and remove are no-ops when already done, and every
// command is refused without the flag.
func TestBridgeSimFlag(t *testing.T) {
	plain := newFleet()
	for _, name := range bridgeSimCommands {
		err := plain.applyPipeCommand(pipeCommand{Name: name})
		if err == nil || !strings.Contains(err.Error(), "--bridge-sim") {
			t.Errorf("%s without --bridge-sim: %v, want a refusal naming the flag", name, err)
		}
	}

	f := newFleet()
	sim, err := newDemoBridgeSim(f)
	if err != nil {
		t.Fatal(err)
	}
	f.bridgeSim = sim
	n := testNode(t, f, filepath.Join(t.TempDir(), "bridgesim.db"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sim.numberPowerSource(); err != nil {
		t.Fatal(err)
	}
	br := n.Bridge()
	byID := func(id uint16) *endpoint.Endpoint {
		for _, ep := range br.Topology().Endpoints {
			if ep != nil && ep.ID == id {
				return ep
			}
		}
		return nil
	}
	number := func(d *device.Device) uint16 {
		t.Helper()
		no, ok := d.EndpointNumber()
		if !ok {
			t.Fatalf("%s has no endpoint number", d.Spec().StableKey)
		}
		return no
	}

	// The composition: every simulated endpoint after the daemon's own.
	light1 := mountedEndpoint(t, br, "demo-bridgesim-light1-1")
	for _, ep := range br.Topology().Endpoints {
		if ep != nil && ep.ID > light1.ID && !ep.Part && !strings.HasPrefix(ep.DeviceAddress, "demo-bridgesim-") {
			t.Errorf("endpoint %d (%s) after the simulated bridge's first: the simulation must come last", ep.ID, ep.DeviceAddress)
		}
	}
	composed := byID(number(sim.composed.Device))
	partNos := []uint16{number(sim.sensors[2].Device), number(sim.sensors[3].Device)}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint < light1.ID {
			continue
		}
		// The composed device's sensors share a device type, so Base
		// requires a TagList of each (matter.js gives them
		// CommonNumberTag One and Two, BridgeTestInstance.ts:306-317); the
		// device kit has no TagList declaration (notes/parity/
		// matterjs_tests.md, SemanticNamespaceTest.ts row).
		if slices.Contains(partNos, v.Endpoint) && strings.Contains(v.String(), "Descriptor.TAGLIST") {
			continue
		}
		t.Errorf("device type violation: %s", v)
	}
	types := readMounted(t, composed, descriptordef.ClusterID, descriptordef.AttrDeviceTypeList).([]mattercore.DeviceTypeStruct)
	ids := make([]uint32, 0, len(types))
	for _, dt := range types {
		ids = append(ids, dt.DeviceType)
	}
	if !slices.Contains(ids, uint32(device.PowerSourceID)) || !slices.Contains(ids, uint32(device.BridgedNodeID)) {
		t.Errorf("composed DeviceTypeList %v, want Power Source 0x0011 and Bridged Node 0x0013 (:330-335)", ids)
	}
	if parts := readMounted(t, composed, descriptordef.ClusterID, descriptordef.AttrPartsList); !slices.Equal(parts.([]uint16), partNos) {
		t.Errorf("composed PartsList %v, want its two sensors %v", parts, partNos)
	}
	for _, no := range partNos {
		part := byID(no)
		if !part.Part || part.DeviceType != uint16(device.TemperatureSensorID) {
			t.Errorf("composed part %d: part %v, device type 0x%04X", no, part.Part, part.DeviceType)
		}
		for _, srv := range endpoint.ClusterServers(part) {
			if srv.MatterClusterID() == bdbidef.ClusterID {
				t.Errorf("composed part %d serves BridgedDeviceBasicInformation (:159-161)", no)
			}
		}
	}
	if got := readMounted(t, composed, psdef.ClusterID, psdef.AttrEndpointList); !slices.Equal(got.([]uint16), append([]uint16{composed.ID}, partNos...)) {
		t.Errorf("composed EndpointList %v, want %d and its parts %v (:344-348)", got, composed.ID, partNos)
	}
	if got := sim.composed.PowerSource().Get(); got.BatChargeLevel != psdef.BatChargeLevelOk || got.Description != "Battery" {
		t.Errorf("composed PowerSource BatChargeLevel %v, Description %q", got.BatChargeLevel, got.Description)
	}
	if label := readMounted(t, composed, bdbidef.ClusterID, bdbidef.AttrNodeLabel); label != bridgeSimComposedLabel {
		t.Errorf("composed NodeLabel %v", label)
	}

	// toggleBridgedLights: Light 1 only; Light 2 is not there yet.
	on := sim.light1.light.isOn()
	if err := f.applyPipeCommand(pipeCommand{Name: "toggleBridgedLights"}); err != nil {
		t.Fatal(err)
	}
	if got := readMounted(t, light1, onoffdef.ClusterID, onoffdef.AttrOnOff); got != !on {
		t.Errorf("Light 1 OnOff %v after the toggle, want %v", got, !on)
	}
	for _, l := range sim.actionLights {
		if l.OnOff().Get().OnOff {
			t.Errorf("%s toggled", l.Spec().FriendlyName)
		}
	}

	// warmBridgedTemperatureSensors: each of the four one degree up.
	if err := f.applyPipeCommand(pipeCommand{Name: "warmBridgedTemperatureSensors"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range sim.sensors {
		got := readMounted(t, byID(number(s.Device)), tmdef.ClusterID, tmdef.AttrMeasuredValue)
		if v, ok := got.(int16); !ok || v != 200 {
			t.Errorf("sensor %d MeasuredValue %#v after the warm, want 200", number(s.Device), got)
		}
	}

	// renameBridgedLights: "Light 1b".
	if err := f.applyPipeCommand(pipeCommand{Name: "renameBridgedLights"}); err != nil {
		t.Fatal(err)
	}
	if got := readMounted(t, byID(light1.ID), bdbidef.ClusterID, bdbidef.AttrNodeLabel); got != "Light 1b" {
		t.Errorf("Light 1 NodeLabel %v after the rename", got)
	}

	// addBridgedLight: a fresh number, named by the root and the
	// Aggregator; a second add changes nothing.
	for range 2 {
		if err := f.applyPipeCommand(pipeCommand{Name: "addBridgedLight"}); err != nil {
			t.Fatal(err)
		}
	}
	light2 := number(sim.light2.dev.Device)
	for _, ep := range br.Topology().Endpoints {
		if ep != nil && ep.ID != light2 && ep.ID >= light2 {
			t.Errorf("Light 2 at %d, but endpoint %d exists: the added light takes a number no device had", light2, ep.ID)
		}
	}
	for _, at := range []uint16{0, 1} {
		if parts := readMounted(t, byID(at), descriptordef.ClusterID, descriptordef.AttrPartsList).([]uint16); !slices.Contains(parts, light2) {
			t.Errorf("endpoint %d PartsList %v lacks the added light %d", at, parts, light2)
		}
	}

	// removeBridgedLight: Light 1 gone from the topology and both
	// PartsLists; a second remove changes nothing; Light 2 stays.
	for range 2 {
		if err := f.applyPipeCommand(pipeCommand{Name: "removeBridgedLight"}); err != nil {
			t.Fatal(err)
		}
	}
	if byID(light1.ID) != nil {
		t.Errorf("Light 1 (%d) still in the topology after the remove", light1.ID)
	}
	for _, at := range []uint16{0, 1} {
		if parts := readMounted(t, byID(at), descriptordef.ClusterID, descriptordef.AttrPartsList).([]uint16); slices.Contains(parts, light1.ID) || !slices.Contains(parts, light2) {
			t.Errorf("endpoint %d PartsList %v after the remove: want %d gone, %d kept", at, parts, light1.ID, light2)
		}
	}

	// The toggle now reaches Light 2 alone.
	if err := f.applyPipeCommand(pipeCommand{Name: "toggleBridgedLights"}); err != nil {
		t.Fatal(err)
	}
	if !sim.light2.light.isOn() {
		t.Error("Light 2 not toggled on")
	}
}
