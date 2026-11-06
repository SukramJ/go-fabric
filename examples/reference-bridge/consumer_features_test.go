// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	co2def "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestGarageServesTagList: the garage's Closure endpoint opts into the
// Descriptor's TagList, so it meets its device type without the
// TAGLIST departure (BD-Matter-ClosureWithoutTagList) and TC-DESC-2.2
// reads a tag list on a bridged endpoint.
func TestGarageServesTagList(t *testing.T) {
	_, br := startFleetBridge(t)
	garage := mountedEndpoint(t, br, "demo-garage-1")
	if fm := readMounted(t, garage, descdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(descdef.FeatureTagList) {
		t.Errorf("Descriptor FeatureMap = %v, want TAGLIST", fm)
	}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint == garage.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
}

// TestAirQualitySensorServesEveryConcentrationFeature: the CO2 cluster
// serves every feature with its readings, and AirQuality every optional
// grade.
func TestAirQualitySensorServesEveryConcentrationFeature(t *testing.T) {
	_, br := startFleetBridge(t)
	ep := mountedEndpoint(t, br, "demo-airquality-1")
	if fm := readMounted(t, ep, co2def.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(0x3F) {
		t.Errorf("CO2 FeatureMap = %v, want MEA|LEV|MED|CRI|PEA|AVG", fm)
	}
	for id, want := range map[uint32]any{
		co2def.AttrPeakMeasuredValueWindow: co2PeakWindowS, co2def.AttrAverageMeasuredValueWindow: co2AverageWindowS,
		co2def.AttrLevelValue: uint8(co2def.LevelValueLow),
	} {
		if v := readMounted(t, ep, co2def.ClusterID, id); v != want {
			t.Errorf("CO2 attribute 0x%04X = %v (%T), want %v", id, v, v, want)
		}
	}
	if fm := readMounted(t, ep, aqdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(0x0F) {
		t.Errorf("AirQuality FeatureMap = %v, want FAIR|MOD|VPOOR|XPOOR", fm)
	}
}

// TestColorLightServesTwoPrimaries: the extended colour light's
// NumberOfPrimaries is the two primaries it supplies, as TC-CC-2.1 step 24
// checks.
func TestColorLightServesTwoPrimaries(t *testing.T) {
	_, br := startFleetBridge(t)
	ep := mountedEndpoint(t, br, "demo-colorlight-1")
	for id, want := range map[uint32]any{
		ccdef.AttrNumberOfPrimaries: uint8(2), ccdef.AttrPrimary1X: primaryRedX, ccdef.AttrPrimary1Intensity: primaryRedIntensity,
		ccdef.AttrPrimary2Y: primaryGreenY, ccdef.AttrPrimary2Intensity: nil,
	} {
		if v := readMounted(t, ep, ccdef.ClusterID, id); v != want {
			t.Errorf("ColorControl attribute 0x%04X = %v, want %v", id, v, want)
		}
	}
}
