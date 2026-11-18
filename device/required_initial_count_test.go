// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"testing"
)

// TestRequiredInitialSetIsPinned (N5b-afacade-1, decision 203): decision
// 17's refusal set as built — the generated device types New refuses
// when built bare, each for an attribute matter.js requires a value of at
// initialisation — is 21 types and 36 attributes. A regeneration that
// moves the set fails here, so the CHANGELOG and the migration guide
// (docs/guides/migrating-to-the-facade.md) are corrected with it. Types
// refused first for another reason (a feature selection, no default
// server) are not in the count.
func TestRequiredInitialSetIsPinned(t *testing.T) {
	t.Parallel()
	want := []string{
		"AirPurifier", "AirQualitySensor", "BasicVideoPlayer", "Camera", "CastingVideoPlayer", "Chime",
		"ContentApp", "Dishwasher", "EnergyEvse", "ExtractorHood", "Fan", "Intercom",
		"JointFabricAdministrator", "LaundryDryer", "LaundryWasher", "ModeSelect",
		"NetworkInfrastructureManager", "RoboticVacuumCleaner", "SoilSensor", "ThreadBorderRouter", "WaterHeater",
	}
	const wantRows = 36
	var got []string
	rows := 0
	for _, ty := range Types() {
		req, err := ty.RequiredInitial()
		if err != nil || len(req) == 0 {
			continue
		}
		got = append(got, ty.Name)
		rows += len(req)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) || rows != wantRows {
		t.Errorf("refused bare: %d types %v, %d attributes; want %d types %v, %d attributes",
			len(got), got, rows, len(want), want, wantRows)
	}
}
