// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	rvcrdef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	sadef "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
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

	mu  sync.Mutex
	srv *sadef.Server
	run *rvcrdef.Server
}

var (
	_ sadef.Selector = (*vacuumArea)(nil)
	_ sadef.Skipper  = (*vacuumArea)(nil)
)

func newVacuumArea(v *demoVacuum) *vacuumArea { return &vacuumArea{v: v} }

// declaration declares the vacuum's ServiceArea, the areas as the kit's
// server's Selector and Skipper.
func (a *vacuumArea) declaration() device.Declaration {
	maps := []sadef.MapStruct{{MapId: vacuumMap, Name: "Map0"}}
	areas := []sadef.AreaStruct{
		vacuumAreaEntry(areaKitchen, "Kitchen", tagKitchen),
		vacuumAreaEntry(areaLivingRoom, "Living Room", tagLivingRoom),
	}
	return sadef.Declare{
		Features: sadef.FeatureMaps | sadef.FeatureProgressReporting,
		Selector: a, Skipper: a,
		Initial: sadef.Patch{
			SupportedMaps: &maps, SupportedAreas: &areas,
			Null: []uint32{sadef.AttrCurrentArea, sadef.AttrEstimatedEndTime}, // served, null
		},
	}
}

// bind keeps the vacuum's ServiceArea and starts following its run mode:
// TestServiceAreaServer #reactOnModeChange — back to Idle (mode 0) marks
// every Progress entry Skipped.
func (a *vacuumArea) bind(d *device.RoboticVacuumCleaner) {
	a.mu.Lock()
	a.srv, a.run = sadef.WrapOn(d.Server(sadef.ClusterID), nil), d.RvcRunMode()
	a.mu.Unlock()
	a.run.OnChanged(func(st rvcrdef.State, change contract.Change) {
		if slices.Contains(change.Attributes, rvcrdef.AttrCurrentMode) && st.CurrentMode == rvcIdle {
			a.skipAll()
		}
	})
}

func vacuumAreaEntry(id uint32, name string, tag uint8) sadef.AreaStruct {
	return sadef.AreaStruct{
		AreaId: id,
		MapId:  spec.ValueOf(vacuumMap),
		AreaInfo: sadef.AreaInfoStruct{
			LocationInfo: spec.ValueOf(sadef.Locationdesc{
				LocationName: name, FloorNumber: spec.ValueOf[int16](0), AreaType: spec.ValueOf(tag),
			}),
			LandmarkInfo: spec.NullOf[sadef.LandmarkInfoStruct](),
		},
	}
}

// servers are the ServiceArea and the run mode; nil before the vacuum is
// built.
func (a *vacuumArea) servers() (areas *sadef.Server, run *rvcrdef.Server) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.srv, a.run
}

// runMode is the vacuum's current RvcRunMode.
func (a *vacuumArea) runMode() uint8 {
	_, run := a.servers()
	return run.Get().CurrentMode
}

// skipAll marks every Progress entry Skipped.
func (a *vacuumArea) skipAll() {
	srv, _ := a.servers()
	progress := srv.Get().Progress
	if len(progress) == 0 {
		return
	}
	for i := range progress {
		progress[i].Status = sadef.OperationalStatusSkipped
	}
	if err := srv.Set(sadef.Patch{Progress: &progress}); err != nil {
		slog.Warn("vacuum.area.progress", slog.Any("err", err))
	}
}

// SelectAreas implements [sadef.Selector]: no selection while the run mode
// is Cleaning (TestServiceAreaServer.selectAreas).
func (a *vacuumArea) SelectAreas(_ context.Context, areas []uint32) (status sadef.SelectAreasStatus, text string, err error) {
	if a.runMode() == rvcCleaning {
		return sadef.SelectAreasStatusInvalidInMode, "Cleaning mode does not support area selection", nil
	}
	slog.Info("vacuum.areas", slog.String("device", a.v.name), slog.Any("areas", areas))
	return sadef.SelectAreasStatusSuccess, "", nil
}

// SkipArea implements [sadef.Skipper]: no skip while Idle; otherwise the
// area's Progress entry becomes Skipped (TestServiceAreaServer.skipArea).
func (a *vacuumArea) SkipArea(_ context.Context, area uint32) (status sadef.SkipAreaStatus, text string, err error) {
	if a.runMode() == rvcIdle {
		return sadef.SkipAreaStatusInvalidInMode, "Idle mode does not support area skipping", nil
	}
	srv, _ := a.servers()
	progress := srv.Get().Progress
	for i := range progress {
		if progress[i].AreaId == area {
			progress[i].Status = sadef.OperationalStatusSkipped
		}
	}
	if err := srv.Set(sadef.Patch{Progress: &progress}); err != nil {
		return 0, "", err
	}
	slog.Info("vacuum.skip", slog.String("device", a.v.name), slog.Uint64("area", uint64(area)))
	return sadef.SkipAreaStatusSuccess, "", nil
}

// reset is RvcTestInstance's "reset" for the areas: nothing selected
// (RvcTestInstance.ts:82-93).
func (a *vacuumArea) reset() error {
	srv, _ := a.servers()
	if srv == nil {
		return errNotBuilt
	}
	none := []uint32{}
	return srv.Set(sadef.Patch{SelectedAreas: &none})
}
