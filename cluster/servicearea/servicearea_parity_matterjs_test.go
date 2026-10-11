// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_ServiceAreaServer builds every feature selection with
// every optional element set and holds each server against the matter.js
// conformance of the definition (spectest.CheckServer): FeatureMap,
// ClusterRevision, the attribute and command lists, the privileges and
// the read-only writes.
func TestParityMatterJS_ServiceAreaServer(t *testing.T) {
	t.Parallel()
	for features := range servicearea.Feature(1 << 3) {
		for optional := range 1 << 3 {
			cfg := servicearea.Config{
				Features:       features,
				Selector:       &device{},
				SupportedAreas: []servicearea.Area{area(1, nil, "Kitchen")},
			}
			if features&servicearea.FeatureMaps != 0 {
				cfg.SupportedMaps = []servicearea.Map{{MapId: 0, Name: "Ground"}}
				cfg.SupportedAreas = []servicearea.Area{area(1, ptr[uint32](0), "Kitchen")}
			}
			cfg.CurrentArea = optional&1 != 0
			cfg.EstimatedEndTime = optional&3 == 3
			if optional&4 != 0 && (cfg.CurrentArea || features&servicearea.FeatureProgressReporting != 0) {
				cfg.Skipper = &device{}
			}
			srv, err := servicearea.New(cfg)
			if err != nil {
				t.Fatalf("features 0b%03b optional 0b%03b: %v", features, optional, err)
			}
			spectest.CheckServer(t, srv, sa.Definition, uint32(features))
		}
	}
}

// TestParityMatterJS_ServiceAreaSnapshot pins the snapshot facts the
// server's own rules rest on: the conformance of the optional elements it
// gates (CurrentArea "desc", EstimatedEndTime "[CurrentArea]", SkipArea
// "[CurrentArea | Progress]", SupportedMaps MAPS, Progress PROG), the
// response each command answers with, and the RoboticVacuumCleaner device
// type offering the cluster.
func TestParityMatterJS_ServiceAreaSnapshot(t *testing.T) {
	t.Parallel()
	spectest.CheckDefinition(t, sa.Definition)
	js := paritytest.ClusterSnapshot(t, servicearea.ClusterID)
	for name, conf := range map[string]string{
		"SupportedMaps":    "MAPS",
		"CurrentArea":      "desc",
		"EstimatedEndTime": "[CurrentArea]",
		"Progress":         "PROG",
	} {
		if a := js.Attribute(t, name); a.Conformance != conf {
			t.Errorf("%s conformance %q, want %q", name, a.Conformance, conf)
		}
	}
	if a := js.Attribute(t, "EstimatedEndTime"); a.Quality != "X Q" {
		t.Errorf("EstimatedEndTime quality %q", a.Quality)
	}
	if c := js.Command(t, "SelectAreas"); c.Conformance != "M" || c.Response != "SelectAreasResponse" {
		t.Errorf("SelectAreas %+v", c)
	}
	if c := js.Command(t, "SkipArea"); c.Conformance != "[CurrentArea | Progress]" || c.Response != "SkipAreaResponse" {
		t.Errorf("SkipArea %+v", c)
	}
	if allowed, known := schema.DeviceTypeAllowsServerCluster(uint32(servicearea.DeviceTypeRoboticVacuumCleaner), servicearea.ClusterID); !allowed || !known {
		t.Error("RoboticVacuumCleaner does not offer ServiceArea")
	}
}

// TestParityMatterJS_ServiceAreaPayloads round-trips the command payloads
// and the struct attributes through the generated codecs.
func TestParityMatterJS_ServiceAreaPayloads(t *testing.T) {
	t.Parallel()
	spectest.RoundTripCommand(t, sa.Definition, sa.CmdSelectAreas, spec.Request, sa.SelectAreasRequest{NewAreas: []uint32{1, 2}})
	spectest.RoundTripCommand(t, sa.Definition, sa.CmdSkipArea, spec.Request, sa.SkipAreaRequest{SkippedArea: 7})
	spectest.RoundTripCommand(t, sa.Definition, sa.CmdSelectAreasResponse, spec.Response, sa.SelectAreasResponse{Status: servicearea.SelectAreasInvalidInMode, StatusText: "busy"})
	spectest.RoundTripCommand(t, sa.Definition, sa.CmdSkipAreaResponse, spec.Response, sa.SkipAreaResponse{Status: servicearea.SkipAreaInvalidSkippedArea})
	spectest.RoundTripAttribute(t, sa.Definition, sa.AttrSupportedAreas, spec.List[servicearea.Area]{area(1, ptr[uint32](0), "Kitchen"), landmark(2)})
	spectest.RoundTripAttribute(t, sa.Definition, sa.AttrSupportedMaps, spec.List[servicearea.Map]{{MapId: 0, Name: "Ground"}})
	spectest.RoundTripAttribute(t, sa.Definition, sa.AttrProgress, spec.List[servicearea.Progress]{{AreaId: 1, Status: servicearea.StatusOperating}})
}
