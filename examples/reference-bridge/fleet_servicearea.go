// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
)

// --- the robot vacuum's areas ----------------------------------------------

// The vacuum's map and areas: two areas of matter.js's RVC test node, on
// one map (support/chip-testing/src/RvcTestInstance.ts serviceArea:
// Kitchen 7 and Living Room 1234567, floor 0, with their Common Area
// namespace tags — packages/node/src/tags/CommonAreaNamespaceTag.ts:112
// Kitchen 0x2F, :122 LivingRoom 0x34).
const (
	areaKitchen    uint32 = 7
	areaLivingRoom uint32 = 1234567
	vacuumMap      uint32 = 0

	tagKitchen    uint8 = 0x2F
	tagLivingRoom uint8 = 0x34
)

// vacuumArea is the vacuum's ServiceArea, served on the vacuum's endpoint:
// MAPS and PROG with CurrentArea, EstimatedEndTime and SkipArea, as
// matter.js's RVC test node enables it (RvcTestInstance.ts:38,
// TestServiceAreaServer.with(Maps, ProgressReporting)). The device's
// decisions are that node's (support/chip-testing/src/cluster/
// TestServiceAreaServer.ts): no selection while cleaning, no skip while
// idle, and every Progress entry Skipped when the run mode returns to
// Idle.
type vacuumArea struct {
	v *demoVacuum

	once    sync.Once
	srv     *servicearea.Server
	version cluster.DataVersionTracker
}

var (
	_ servicearea.Selector = (*vacuumArea)(nil)
	_ servicearea.Skipper  = (*vacuumArea)(nil)
)

func newVacuumArea(v *demoVacuum) *vacuumArea { return &vacuumArea{v: v} }

// server builds the ServiceArea server once and starts following the run
// mode.
func (a *vacuumArea) server() *servicearea.Server {
	a.once.Do(func() {
		srv, err := servicearea.New(servicearea.Config{
			Features: servicearea.FeatureMaps | servicearea.FeatureProgressReporting,
			Selector: a, Skipper: a,
			SupportedMaps: []servicearea.Map{{MapId: vacuumMap, Name: "Map0"}},
			SupportedAreas: []servicearea.Area{
				vacuumAreaEntry(areaKitchen, "Kitchen", tagKitchen),
				vacuumAreaEntry(areaLivingRoom, "Living Room", tagLivingRoom),
			},
			CurrentArea: true, EstimatedEndTime: true,
			DataVersion: &a.version,
		})
		if err != nil {
			panic(fmt.Sprintf("vacuum ServiceArea: %v", err))
		}
		a.srv = srv
		a.v.build()
		// TestServiceAreaServer #reactOnModeChange: back to Idle (mode 0)
		// marks every Progress entry Skipped.
		a.v.run.OnMatterAttributesChanged(func(ids []uint32) {
			if slices.Contains(ids, modebase.AttrCurrentMode) && a.runMode() == rvcIdle {
				a.skipAll()
			}
		})
	})
	return a.srv
}

func vacuumAreaEntry(id uint32, name string, tag uint8) servicearea.Area {
	return servicearea.Area{
		AreaId: id,
		MapId:  spec.ValueOf(vacuumMap),
		AreaInfo: servicearea.AreaInfo{
			LocationInfo: spec.ValueOf(servicearea.LocationInfo{
				LocationName: name, FloorNumber: spec.ValueOf[int16](0), AreaType: spec.ValueOf(tag),
			}),
			LandmarkInfo: spec.NullOf[servicearea.LandmarkInfo](),
		},
	}
}

// runMode is the vacuum's current RvcRunMode.
func (a *vacuumArea) runMode() uint8 {
	raw, _ := a.v.run.MatterRead(modebase.AttrCurrentMode)
	mode, _ := raw.(uint8)
	return mode
}

// skipAll marks every Progress entry Skipped.
func (a *vacuumArea) skipAll() {
	raw, _ := a.srv.MatterRead(0x0005) // Progress
	progress, _ := raw.(spec.List[servicearea.Progress])
	if len(progress) == 0 {
		return
	}
	for i := range progress {
		progress[i].Status = servicearea.StatusSkipped
	}
	if err := a.srv.SetProgress(progress); err != nil {
		slog.Warn("vacuum.area.progress", slog.Any("err", err))
	}
}

// SelectAreas implements [servicearea.Selector]: no selection while the
// run mode is Cleaning (TestServiceAreaServer.selectAreas).
func (a *vacuumArea) SelectAreas(_ context.Context, areas []uint32) (status servicearea.SelectAreasStatus, text string, err error) {
	if a.runMode() == rvcCleaning {
		return servicearea.SelectAreasInvalidInMode, "Cleaning mode does not support area selection", nil
	}
	slog.Info("vacuum.areas", slog.String("device", a.v.name), slog.Any("areas", areas))
	return servicearea.SelectAreasSuccess, "", nil
}

// SkipArea implements [servicearea.Skipper]: no skip while Idle; otherwise
// the area's Progress entry becomes Skipped (TestServiceAreaServer.skipArea).
func (a *vacuumArea) SkipArea(_ context.Context, area uint32) (status servicearea.SkipAreaStatus, text string, err error) {
	if a.runMode() == rvcIdle {
		return servicearea.SkipAreaInvalidInMode, "Idle mode does not support area skipping", nil
	}
	raw, _ := a.srv.MatterRead(0x0005) // Progress
	progress, _ := raw.(spec.List[servicearea.Progress])
	for i := range progress {
		if progress[i].AreaId == area {
			progress[i].Status = servicearea.StatusSkipped
		}
	}
	if err := a.srv.SetProgress(progress); err != nil {
		return 0, "", err
	}
	slog.Info("vacuum.skip", slog.String("device", a.v.name), slog.Uint64("area", uint64(area)))
	return servicearea.SkipAreaSuccess, "", nil
}

// reset is RvcTestInstance's "reset" for the areas: nothing selected
// (RvcTestInstance.ts:82-93).
func (a *vacuumArea) reset() error {
	return a.server().SetSelectedAreas(nil)
}

// vacuumWithArea is the vacuum's endpoint source: the vacuum's own servers
// and its ServiceArea.
type vacuumWithArea struct {
	*demoVacuum
	area *vacuumArea
}

// MatterClusterServers implements [contract.EndpointSource].
func (v vacuumWithArea) MatterClusterServers() []contract.ClusterServer {
	return append(v.demoVacuum.MatterClusterServers(), v.area.server())
}
