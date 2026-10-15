// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestStateForecastEsa: the second ESA is a bridged DeviceEnergyManagement
// endpoint with FeatureMap 0x7C (SFR|STA|PAU|FA|CON, TC_DEM_2_6.py:30 at
// the harness pin), states ControllableEsa, mounts its mode cluster, passes
// the device-type validator, and is numbered above the grid meter — the
// harness resolves it there (internal/chiptool dtStateForecastEsa).
func TestStateForecastEsa(t *testing.T) {
	_, br := startFleetBridge(t)
	esa := mountedEndpoint(t, br, "demo-esa-1")
	if esa.DeviceType != deviceTypeDeviceEnergyManagement || !slices.Equal(esa.DeviceConditions, []string{conditionControllableEsa}) {
		t.Fatalf("esa endpoint %+v", esa)
	}
	if fm := readMounted(t, esa, demdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(0x7C) {
		t.Errorf("DeviceEnergyManagement FeatureMap = %v, want 0x7C", fm)
	}
	readMounted(t, esa, modebase.ClusterIDDeviceEnergyManagementMode, modebase.AttrCurrentMode)
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint == esa.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
	meter := mountedEndpoint(t, br, "demo-electricalmeter-1")
	charger := mountedEndpoint(t, br, "demo-evse-1")
	if esa.ID <= meter.ID || esa.ID <= charger.ID {
		t.Errorf("esa endpoint %d, meter %d, charger %d: the ESA must come after both", esa.ID, meter.ID, charger.ID)
	}
	// No DeviceEnergyManagement endpoint lies between the meter and the
	// ESA, so the harness's endpointAfter(meter, 0x050D) lands on it.
	for _, ep := range br.Topology().Endpoints {
		if ep.ID > meter.ID && ep.ID < esa.ID && ep.DeviceType == deviceTypeDeviceEnergyManagement {
			t.Errorf("endpoint %d (DeviceEnergyManagement) lies between the meter and the ESA", ep.ID)
		}
	}
}

// TestDemTriggerReachesBothEsas: a DEM trigger carries no endpoint
// (DeviceEnergyManagementTestEventTriggerHandler.h:43-83), so the daemon's
// dispatch hands it to both DeviceEnergyManagement servers: the ESA and the
// charger's part.
func TestDemTriggerReachesBothEsas(t *testing.T) {
	f, _ := startFleetBridge(t)
	f.evse.build()
	if err := f.testEventTrigger(context.Background(), triggerDemPausable); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	for name, d := range map[string]*demoDem{"esa": f.esa.dem, "charger": f.evse.dem} {
		if fc := d.srv.Forecast(); fc == nil || len(fc.Slots) != 2 {
			t.Errorf("%s forecast after the pausable trigger: %+v", name, fc)
		}
	}
	// SFR: the ESA's slots carry ManufacturerESAState (DEMTestEventTriggers.cpp
	// ConfigureForecast), the charger's (no SFR) do not.
	if fc := f.esa.dem.srv.Forecast(); fc == nil || fc.Slots[0].ManufacturerEsaState == nil {
		t.Errorf("ESA slot 0 has no ManufacturerESAState: %+v", fc)
	}
	if fc := f.evse.dem.srv.Forecast(); fc == nil || fc.Slots[0].ManufacturerEsaState != nil {
		t.Errorf("charger slot 0 carries ManufacturerESAState: %+v", fc)
	}
}
