// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/service-area/ServiceAreaServerTest.ts
// at 862ad75f: a robotic vacuum cleaner whose ServiceArea starts with or
// without EstimatedEndTime, without the Maps feature, and with supported
// areas that pass or break #assertSupportedAreas.
//
// matter.js adds RoboticVacuumCleanerDevice.with(ServiceAreaServer.with(…))
// to a MockServerNode; the port builds the Go device of that type with the
// servicearea server (device.WithServer) and starts it as a fabrictest
// device under the Aggregator. matter.js's node.add rejecting with
// "Behaviors have errors" is servicearea.New returning ErrInvalidState:
// the Go server checks its initial state when it is built, before a node
// serves it. Two Go forms differ from the matter.js options, the rules the
// cases test unchanged:
//
//   - "estimatedEndTime: undefined" and the default state are one Go
//     configuration: EstimatedEndTime not served (Config.EstimatedEndTime
//     false);
//   - the Go server requires a Selector (servicearea.ErrNoSelector) where
//     matter.js's selectAreas has a default; the port passes one that
//     accepts every selection, and no case selects.
//
// The RvcRunMode and RvcOperationalState options of createNode
// (ServiceAreaServerTest.ts:27-50) are the device type's other mandatory
// servers; the device layer builds them with their defaults.
package servicearea_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	devicelayer "github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// Common Area Namespace tags (packages/node/src/tags/CommonAreaNamespaceTag.ts
// at 862ad75f: Bathroom :31, Bedroom :33, Kitchen :112, LivingRoom :122).
const (
	tagBathroom   uint8 = 0x06
	tagBedroom    uint8 = 0x07
	tagKitchen    uint8 = 0x2F
	tagLivingRoom uint8 = 0x34
)

// mapsAndSelectWhileRunning is ServiceAreaServerTest.ts:16 DeviceType's
// ServiceAreaServer.with("Maps", "SelectWhileRunning").
const mapsAndSelectWhileRunning = servicearea.FeatureMaps | servicearea.FeatureSelectWhileRunning

// createNode is ServiceAreaServerTest.ts:20-55: a node serving a robotic
// vacuum cleaner with the ServiceArea of cfg.
func createNode(t *testing.T, cfg servicearea.Config) error {
	t.Helper()
	cfg.Selector = &device{}
	srv, err := servicearea.New(cfg)
	if err != nil {
		return err
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	rvc, err := devicelayer.NewRoboticVacuumCleaner(devicelayer.Options{StableKey: endpoint.StringKey("rvc:1"), Name: "rvc"},
		devicelayer.WithServer(srv))
	if err != nil {
		t.Fatal(err)
	}
	d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{rvc}})
	if _, ok := d.EndpointOf("rvc:1"); !ok {
		t.Fatal("the vacuum cleaner has no endpoint")
	}
	return nil
}

// locatedArea is one supportedAreas entry of ServiceAreaServerTest.ts:111-160
// and :180-229: a location on a map, without a landmark.
func locatedArea(id, mapID uint32, name string, floor int16, areaType uint8) servicearea.Area {
	return servicearea.Area{
		AreaId: id,
		MapId:  spec.ValueOf(mapID),
		AreaInfo: servicearea.AreaInfo{
			LocationInfo: spec.ValueOf(servicearea.LocationInfo{
				LocationName: name, FloorNumber: spec.ValueOf(floor), AreaType: spec.ValueOf(areaType),
			}),
			LandmarkInfo: spec.NullOf[servicearea.LandmarkInfo](),
		},
	}
}

// twoFloors is the supportedMaps of both supportedAreas cases.
var twoFloors = []servicearea.Map{{MapId: 1, Name: "Ground floor"}, {MapId: 2, Name: "First floor"}}

func TestParityMatterJS_ServiceAreaServerTest(t *testing.T) {
	t.Run("allows undefined estimatedEndTime", func(t *testing.T) {
		if err := createNode(t, servicearea.Config{Features: mapsAndSelectWhileRunning}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("allows empty estimatedEndTime", func(t *testing.T) {
		if err := createNode(t, servicearea.Config{Features: mapsAndSelectWhileRunning}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("initializes without the Maps feature", func(t *testing.T) {
		// RoboticVacuumCleanerDevice.with(ServiceAreaServer): no feature.
		if err := createNode(t, servicearea.Config{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("correctly validate supportedAreas", func(t *testing.T) {
		// Area 2 lies on map 3, which supportedMaps does not list:
		// #assertSupportedAreas does not check map membership
		// (ServiceAreaServer.ts:94-147).
		err := createNode(t, servicearea.Config{
			Features:      mapsAndSelectWhileRunning,
			SupportedMaps: twoFloors,
			SupportedAreas: []servicearea.Area{
				locatedArea(1, 1, "Living", 0, tagLivingRoom),
				locatedArea(2, 3, "Kitchen", 0, tagKitchen),
				locatedArea(3, 2, "Bedroom", 1, tagBedroom),
				locatedArea(4, 2, "Bathroom", 1, tagBathroom),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects with invalid supportedAreas", func(t *testing.T) {
		// Areas 3 and 4 share map 2 and an equal AreaInfo.
		err := createNode(t, servicearea.Config{
			Features:      mapsAndSelectWhileRunning,
			SupportedMaps: twoFloors,
			SupportedAreas: []servicearea.Area{
				locatedArea(1, 1, "Living", 0, tagLivingRoom),
				locatedArea(2, 3, "Kitchen", 0, tagKitchen),
				locatedArea(3, 2, "Bedroom", 1, tagBedroom),
				locatedArea(4, 2, "Bedroom", 1, tagBedroom),
			},
		})
		if !errors.Is(err, servicearea.ErrInvalidState) {
			t.Fatalf("createNode: %v, want ErrInvalidState", err)
		}
	})
}
