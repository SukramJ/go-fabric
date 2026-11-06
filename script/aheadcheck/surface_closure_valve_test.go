// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/surface"
)

// measureMover is a ClosureControl Mover with every capability interface.
type measureMover struct{}

func (measureMover) MoveTo(context.Context, ccldef.TargetPositionEnum) error { return nil }
func (measureMover) Stop(context.Context) error                              { return nil }
func (measureMover) LatchControlModes() ccldef.LatchControlModesBitmap       { return 0 }
func (measureMover) Latch(context.Context, bool) error                       { return nil }
func (measureMover) SetSpeed(context.Context, ccldef.ThreeLevelAutoEnum) error {
	return nil
}
func (measureMover) Calibrate(context.Context) error { return nil }

// measureClosure builds a Positioning ClosureControl through the device
// layer with the row enabled, with or without MotionLatching, Speed and
// Calibration — the row's features but Instantaneous, which Speed's
// "[PS & !IS]" excludes beside them.
func measureClosure(t *testing.T, row string, with bool) surface.Cluster {
	t.Helper()
	features := ccldef.FeaturePositioning
	if with {
		features |= ccldef.Feature(closure.AheadFeatures) &^ ccldef.FeatureInstantaneous
	}
	p := surface.Policies{Extensions: surface.NewExtensions(closure.Ahead.IDs()...)}
	d, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey("cc"), Name: "cc", Surface: p},
		ccldef.Declare{Features: features, Mover: measureMover{}})
	if err != nil {
		t.Fatalf("%s: %v", row, err)
	}
	return endpoint.ClusterSurface(d.Server(ccldef.ClusterID))
}

// measureValveSource is a valve StateSource with a level.
type measureValveSource struct{}

func (measureValveSource) CurrentState() (valvedef.ValveStateEnum, bool)         { return 0, false }
func (measureValveSource) TargetState() (valvedef.ValveStateEnum, bool)          { return 0, false }
func (measureValveSource) OpenDuration() (uint32, bool)                          { return 0, false }
func (measureValveSource) RemainingDuration() (uint32, bool)                     { return 0, false }
func (measureValveSource) DefaultOpenDuration() (uint32, bool)                   { return 0, false }
func (measureValveSource) SetDefaultOpenDuration(context.Context, *uint32) error { return nil }
func (measureValveSource) Open(context.Context, valvedef.OpenRequest) error      { return nil }
func (measureValveSource) Close(context.Context) error                           { return nil }
func (measureValveSource) CurrentLevel() (uint8, bool)                           { return 0, false }
func (measureValveSource) TargetLevel() (uint8, bool)                            { return 0, false }

// measureValve builds a WaterValve's ValveConfigurationAndControl through
// the device layer with the row enabled, with or without Level and
// TimeSync.
func measureValve(t *testing.T, row string, with bool) surface.Cluster {
	t.Helper()
	var features valvedef.Feature
	if with {
		features = valvedef.Feature(valve.FeatureLevel | valve.FeatureTimeSync)
	}
	p := surface.Policies{Extensions: surface.NewExtensions(valve.Ahead.IDs()...)}
	d, err := device.NewWaterValve(device.Options{StableKey: endpoint.StringKey("v"), Name: "v", Surface: p},
		valvedef.Declare{Features: features, StateSource: measureValveSource{}})
	if err != nil {
		t.Fatalf("%s: %v", row, err)
	}
	return endpoint.ClusterSurface(d.Server(valvedef.ClusterID))
}
