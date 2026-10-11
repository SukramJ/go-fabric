// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/spec"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	pscfgdef "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	"github.com/SukramJ/go-fabric/contract"
)

// TestSoilSensorEndpoint: the soil sensor is a SoilSensor endpoint whose
// SoilMeasurement starts at the minimum, follows the app pipe's
// SetSimulatedSoilMoisture (null included), and refuses a command naming
// another endpoint.
func TestSoilSensorEndpoint(t *testing.T) {
	f, br := startFleetBridge(t)
	ep := mountedEndpoint(t, br, "demo-soil-1")
	if ep.DeviceType != contract.MeasurementClassDeviceType(contract.MeasurementSoilMoisture) || ep.DeviceType != 0x0045 {
		t.Fatalf("soil endpoint device type = 0x%04X, want SoilSensor 0x0045", ep.DeviceType)
	}
	limits, ok := readMounted(t, ep, measurement.ClusterSoilMeasurement, soildef.AttrSoilMoistureMeasurementLimits).(soildef.MeasurementAccuracyStruct)
	if !ok || limits.MinMeasuredValue != 0 || limits.MaxMeasuredValue != 100 || len(limits.AccuracyRanges) != 1 {
		t.Fatalf("limits = %#v", limits)
	}
	if v := readMounted(t, ep, measurement.ClusterSoilMeasurement, soildef.AttrSoilMoistureMeasuredValue); v != uint8(0) {
		t.Errorf("initial SoilMoistureMeasuredValue = %v, want the minimum 0", v)
	}

	attachNodeProviders(br, nil, f)
	hundred := uint8(100)
	if err := f.setSimulatedSoilMoisture(ep.ID, &hundred); err != nil {
		t.Fatal(err)
	}
	if v := readMounted(t, ep, measurement.ClusterSoilMeasurement, soildef.AttrSoilMoistureMeasuredValue); v != uint8(100) {
		t.Errorf("SoilMoistureMeasuredValue = %v, want 100", v)
	}
	if err := f.setSimulatedSoilMoisture(ep.ID, nil); err != nil {
		t.Fatal(err)
	}
	if v := readMounted(t, ep, measurement.ClusterSoilMeasurement, soildef.AttrSoilMoistureMeasuredValue); v != nil {
		t.Errorf("SoilMoistureMeasuredValue = %v, want null", v)
	}
	if err := f.setSimulatedSoilMoisture(ep.ID+1, &hundred); !errors.Is(err, errWrongSoilEndpoint) {
		t.Errorf("another endpoint: %v, want errWrongSoilEndpoint", err)
	}
}

// TestNodeClustersOnRootAndAggregator: PowerSourceConfiguration on the
// root lists the EV charger's PowerSource endpoint, the Aggregator's
// Actions lists the desk lamp, and the air quality sensor carries
// EcosystemInformation.
func TestNodeClustersOnRootAndAggregator(t *testing.T) {
	f, br := startFleetBridge(t)
	pscfg, err := mattercore.NewPowerSourceConfiguration(nil)
	if err != nil {
		t.Fatal(err)
	}
	attachNodeProviders(br, pscfg, f)

	power, ok := endpointByKey(br, evsePowerKey)
	if !ok {
		t.Fatal("the EV charger's PowerSource part has no endpoint")
	}
	if v, _ := pscfg.MatterRead(pscfgdef.AttrSources); !slices.Equal(v.([]uint16), []uint16{power}) {
		t.Errorf("Sources = %v, want [%d]", v, power)
	}
	if _, ok := endpointByKey(br, "demo:none:1"); ok {
		t.Error("an unknown key resolved")
	}

	actions, err := mattercore.NewActions(mattercore.ActionsConfig{Host: f.actions})
	if err != nil {
		t.Fatal(err)
	}
	lamp := mountedEndpoint(t, br, "demo-light-1")
	v, _ := actions.MatterRead(actionsdef.AttrEndpointLists)
	lists := v.(spec.List[actionsdef.EndpointListStruct])
	if len(lists) != 1 || !slices.Equal(lists[0].Endpoints, []uint16{lamp.ID}) {
		t.Errorf("EndpointLists = %+v, want the desk lamp's endpoint %d", lists, lamp.ID)
	}
	ctx := context.Background()
	if _, err := actions.MatterInvoke(ctx, actionsdef.CmdInstantAction, actionsdef.InstantActionRequest{ActionId: demoLightsOn}); err != nil {
		t.Fatal(err)
	}
	if !f.light.isOn() {
		t.Error("Lights On left the lamp off")
	}
	if _, err := actions.MatterInvoke(ctx, actionsdef.CmdInstantAction, actionsdef.InstantActionRequest{ActionId: demoLightsOff}); err != nil {
		t.Fatal(err)
	}
	if f.light.isOn() {
		t.Error("Lights Off left the lamp on")
	}
	if _, err := actions.MatterInvoke(ctx, actionsdef.CmdStartAction, actionsdef.StartActionRequest{ActionId: demoLightsOn}); err == nil {
		t.Error("an unsupported command reached the host")
	}

	aq := mountedEndpoint(t, br, "demo-airquality-1")
	devs, ok := readMounted(t, aq, ecodef.ClusterID, ecodef.AttrDeviceDirectory).(spec.List[spec.Encodable])
	if !ok || len(devs) != 1 {
		t.Fatalf("DeviceDirectory = %#v", devs)
	}
	locs, ok := readMounted(t, aq, ecodef.ClusterID, ecodef.AttrLocationDirectory).(spec.List[spec.Encodable])
	if !ok || len(locs) != 1 {
		t.Fatalf("LocationDirectory = %#v", locs)
	}
}

// TestDemoActionsWithoutTopology: before the bridge numbers the lamp, the
// living room lists no endpoint.
func TestDemoActionsWithoutTopology(t *testing.T) {
	t.Parallel()
	a := newDemoActions(nil)
	if l := a.EndpointLists(); len(l) != 1 || len(l[0].Endpoints) != 0 {
		t.Errorf("EndpointLists = %+v", l)
	}
	a.setLampEndpoint(func() (uint16, bool) { return 0, false })
	if l := a.EndpointLists(); len(l[0].Endpoints) != 0 {
		t.Errorf("EndpointLists = %+v", l)
	}
	if err := a.HandleAction(context.Background(), mattercore.ActionRequest{ActionID: demoLightsOn}); err != nil {
		t.Error(err)
	}
	if len(a.ActionList()) != 2 {
		t.Error("want two demo actions")
	}
}
