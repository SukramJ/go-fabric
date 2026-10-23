// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bridgedinfodef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// nullSink takes the thermostat's admitted setpoint changes.
type nullSink struct{}

func (nullSink) SetSystemMode(context.Context, uint8) error { return nil }

func (nullSink) SetSetpoint(context.Context, uint32, int16, []thermostatdef.SetpointChange) error {
	return nil
}

// TestTypedReadOverTheWire reads whole clusters of two bridged devices
// over the wire and holds each generated StateFrom against the State the
// device's typed server reads from the same stored values — the
// controller's typed read and the device's Get are one generated path
// (spec.StateFrom over State.FromAttributeValues).
func TestTypedReadOverTheWire(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewDimmableLight(device.Options{StableKey: device.StringKey("light"), Name: "Hall light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(true)}},
		levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(42))}})
	if err != nil {
		t.Fatal(err)
	}
	thermo, err := device.NewThermostat(device.Options{StableKey: device.StringKey("thermo"), Name: "Living room"},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating,
			Initial:  thermostatdef.Patch{LocalTemperature: new(int16(1950)), OccupiedHeatingSetpoint: new(int16(2100))},
			Sink:     nullSink{},
		})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light, thermo}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lightEP, _ := light.EndpointNumber()
	thermoEP, _ := thermo.EndpointNumber()

	read := func(ep uint16, cluster uint32) spec.AttributeValues {
		t.Helper()
		v, err := fabrictest.ReadCluster(ctx, pair, ep, cluster)
		if err != nil {
			t.Fatalf("read %d/0x%04X: %v", ep, cluster, err)
		}
		return v
	}
	v := read(lightEP, onoffdef.ClusterID)
	gotOnOff, absent := onoffdef.StateFrom(v)
	spectest.EqualState(t, gotOnOff, light.OnOff().Get())
	unserved(t, v, absent)
	v = read(lightEP, levelcontroldef.ClusterID)
	gotLevel, absent := levelcontroldef.StateFrom(v)
	spectest.EqualState(t, gotLevel, light.LevelControl().Get())
	unserved(t, v, absent)
	if l := gotLevel.CurrentLevel; l == nil || *l != 42 {
		t.Errorf("CurrentLevel %v, want 42", l)
	}
	v = read(thermoEP, thermostatdef.ClusterID)
	gotThermo, absent := thermostatdef.StateFrom(v)
	spectest.EqualState(t, gotThermo, thermo.Thermostat().Get())
	unserved(t, v, absent)
	if gotThermo.OccupiedHeatingSetpoint != 2100 {
		t.Errorf("OccupiedHeatingSetpoint %d, want 2100", gotThermo.OccupiedHeatingSetpoint)
	}
	v = read(lightEP, bridgedinfodef.ClusterID)
	gotInfo, absent := bridgedinfodef.StateFrom(v)
	unserved(t, v, absent)
	if gotInfo.NodeLabel != "Hall light" || !gotInfo.Reachable {
		t.Errorf("BridgedDeviceBasicInformation %+v", gotInfo)
	}

	// A cluster the endpoint does not serve: nothing read, all absent.
	none, absent := onoffdef.StateFrom(read(thermoEP, onoffdef.ClusterID))
	if len(absent) == 0 || none.OnOff {
		t.Errorf("OnOff on the thermostat: %+v, absent %v", none, absent)
	}

	// A scalar write over the wire, read back typed.
	if err := fabrictest.Write(ctx, pair, client.AttributeAt(lightEP, onoffdef.ClusterID, onoffdef.AttrOnTime), spec.Uint16(30)); err != nil {
		t.Fatal(err)
	}
	if st, _ := onoffdef.StateFrom(read(lightEP, onoffdef.ClusterID)); st.OnTime != 30 || light.OnOff().Get().OnTime != 30 {
		t.Errorf("OnTime: controller %d, device %d, want 30", st.OnTime, light.OnOff().Get().OnTime)
	}
}

// unserved fails t unless every attribute StateFrom reports absent is one
// the peer's AttributeList does not list: a served attribute always
// arrives in a whole-cluster read.
func unserved(t *testing.T, v spec.AttributeValues, absent []uint32) {
	t.Helper()
	n, ok := v.Value(spec.AttrAttributeList)
	if !ok {
		t.Fatal("the read carries no AttributeList")
	}
	list, err := spec.DecodeAttribute(nil, spec.AttrAttributeList, n)
	if err != nil {
		t.Fatal(err)
	}
	served := map[uint32]bool{}
	for _, id := range list.([]any) {
		served[uint32(id.(uint64))] = true
	}
	for _, id := range absent {
		if served[id] {
			t.Errorf("attribute 0x%04X is served but absent", id)
		}
	}
}
