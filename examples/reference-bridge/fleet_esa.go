// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// --- device: a state-forecast ESA ----------------------------------------------
//
// demoEsa is the second DeviceEnergyManagement the DEM family needs.
// TC-DEM-2.6 and 2.8 run chip's EVSE app with `--featureSet 0x7c`
// (TC_DEM_2_6.py:30, TC_DEM_2_8.py:30 at the harness pin 6170af84):
// StateForecastReporting, StartTimeAdjustment, Pausable,
// ForecastAdjustment and ConstraintBasedAdjustment (bits 2 to 6,
// cluster/spec/deviceenergymanagement/definition_gen.go:69-73), no
// PowerAdjustment and no PowerForecastReporting. Their PICS gates are
// DEM.S.F05 & DEM.S.F02 and DEM.S.F06 & DEM.S.F02; the charger's part
// (fleet_evse.go, PA|PFR|STA|PAU|FA|CON) has no SFR, so neither case
// applies there. Report K "Left out 1" has the design: the charger's
// demoDem with that feature set and the same delegate, modes and
// triggers, as a standalone bridged device stating ControllableEsa.
type demoEsa struct {
	name string
	dem  *demoDem
	// triggers serialises the DEM trigger handler, as the charger's lock
	// does for its part (demoEvse.triggers).
	triggers sync.Mutex
}

func newDemoEsa(name string) *demoEsa {
	return &demoEsa{name: name, dem: newStateForecastDem(name, time.Now)}
}

// newStateForecastDem is newDemoDem (fleet_evse.go) with the feature set
// of TC-DEM-2.6/2.8's app: the same ESA type, power range, delegate and
// modes (EVSEManufacturerImpl.cpp:73-78, device-energy-management-mode.cpp).
func newStateForecastDem(name string, now func() time.Time) *demoDem {
	d := &demoDem{name: name}
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: energy.DemFeatureStateForecastReporting | energy.DemFeatureStartTimeAdjustment |
			energy.DemFeaturePausable | energy.DemFeatureForecastAdjustment | energy.DemFeatureConstraintBasedAdjustment,
		EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000,
		Manager: d, Now: now, DataVersion: &d.version.dem,
	})
	if err != nil {
		panic(fmt.Sprintf("esa DeviceEnergyManagement: %v", err))
	}
	modes, err := modebase.NewDeviceEnergyManagementMode(modebase.Config{
		Changer: d,
		SupportedModes: []modebase.ModeOption{
			{Label: "No energy management (forecast only)", Mode: demModeNoOptimization, Tags: []modebase.ModeTag{{Value: modebase.DemTagNoOptimization}}},
			{Label: "Device optimizes (no local or grid control)", Mode: demModeDeviceOnlyOptimization, Tags: []modebase.ModeTag{{Value: modebase.DemTagDeviceOptimization}}},
			{Label: "Optimized within building", Mode: demModeDeviceAndLocalOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagLocalOptimization}, {Value: modebase.DemTagDeviceOptimization},
			}},
			{Label: "Optimized for grid", Mode: demModeDeviceAndGridOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagDeviceOptimization}, {Value: modebase.DemTagGridOptimization},
			}},
			{Label: "Optimized for grid and building", Mode: demModeAllOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagLocalOptimization}, {Value: modebase.DemTagDeviceOptimization}, {Value: modebase.DemTagGridOptimization},
			}},
		},
		CurrentMode: demModeNoOptimization,
		DataVersion: &d.version.modes,
	})
	if err != nil {
		panic(fmt.Sprintf("esa DeviceEnergyManagementMode: %v", err))
	}
	d.srv, d.modes = srv, modes
	return d
}

// servers are the cluster servers the device supplies.
func (e *demoEsa) servers() []contract.ClusterServer {
	return e.dem.servers()
}

// device builds the ESA's bridged endpoint, stating ControllableEsa so
// DeviceEnergyManagementMode is mounted, as the charger's part does. It is
// appended after the energy-market devices: the harness resolves it as the
// first DeviceEnergyManagement endpoint above the grid meter
// (internal/chiptool dtStateForecastEsa).
func (e *demoEsa) device(opts device.Options) (*device.DeviceEnergyManagement, error) {
	opts.Conditions = []string{conditionControllableEsa}
	return device.NewDeviceEnergyManagement(opts, serve(e.servers())...)
}

// testEventTrigger hands a DEM trigger to the ESA's handler
// (DEMTestEventTriggers.cpp:366-463, demoDem.testEventTrigger) and returns
// false, so the dispatch goes on to the charger: the DEM trigger codes
// carry no endpoint (DeviceEnergyManagementTestEventTriggerHandler.h:43-83),
// and in chip's app the one delegate they reach is the device's only
// DEM. Here the daemon has two, and both answer.
func (e *demoEsa) testEventTrigger(trigger uint64) bool {
	if (trigger&smokeTriggerMask)>>48 != 0x0098 {
		return false
	}
	e.triggers.Lock()
	defer e.triggers.Unlock()
	e.dem.testEventTrigger(trigger & smokeTriggerMask)
	return false
}
