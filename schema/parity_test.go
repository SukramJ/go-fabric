// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestDeviceTypeRevisionLookup verifies the DeviceTypeRevision helper returns
// the expected revisions for the device types the bridge currently advertises.
// Mirrors the cases in cluster/core/parity_matterjs_test.go
// TestParityMatterJS_DeviceTypeRevisions.
func TestDeviceTypeRevisionLookup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id      uint32
		name    string
		wantRev uint16
	}{
		{0x0016, "RootNode", 5},
		{0x000E, "Aggregator", 2},
		{0x0013, "BridgedNode", 3},
		{0x0015, "ContactSensor", 2},
		{0x0043, "WaterLeakDetector", 2},
		{0x002C, "AirQualitySensor", 1},
		{0x0076, "SmokeCoAlarm", 2},
		{0x0106, "LightSensor", 4},
		{0x0107, "OccupancySensor", 4},
		{0x0302, "TemperatureSensor", 3},
		{0x0305, "PressureSensor", 3},
		{0x0307, "HumiditySensor", 3},
		{0x000F, "GenericSwitch", 3},
		{0x0100, "OnOffLight", 4},
		{0x0101, "DimmableLight", 4},
		{0x010A, "OnOffPlugInUnit", 5},
		{0x010C, "ColorTemperatureLight", 5},
		{0x010D, "ExtendedColorLight", 5},
		{0x0202, "WindowCovering", 7},
		{0x0301, "Thermostat", 7},
		{0x000A, "DoorLock", 4},
	}

	for _, c := range cases {
		rev, ok := schema.DeviceTypeRevision(schema.DeviceTypeID(c.id))
		if !ok {
			t.Errorf("device type 0x%04X (%s) not found in generated schema", c.id, c.name)
			continue
		}
		if rev != c.wantRev {
			t.Errorf("device type 0x%04X (%s): schema revision %d != expected %d",
				c.id, c.name, rev, c.wantRev)
		}
	}
}

// TestSchemaCompleteness verifies the generated maps are non-empty and
// contain the baseline cluster + device-type count from matter.js HEAD.
// A sudden drop here means the snapshot was truncated or the generator broke.
func TestSchemaCompleteness(t *testing.T) {
	t.Parallel()

	const (
		minClusters    = 100 // matter.js HEAD has 115; allow some slack for future removals
		minDeviceTypes = 60  // matter.js HEAD has 72
	)

	if got := len(schema.ClusterRevisions); got < minClusters {
		t.Errorf("ClusterRevisions has %d entries, want at least %d — regenerate schema", got, minClusters)
	}
	if got := len(schema.ClusterNames); got != len(schema.ClusterRevisions) {
		t.Errorf("ClusterNames (%d) and ClusterRevisions (%d) have different lengths", len(schema.ClusterNames), len(schema.ClusterRevisions))
	}
	if got := len(schema.DeviceTypeRevisions); got < minDeviceTypes {
		t.Errorf("DeviceTypeRevisions has %d entries, want at least %d — regenerate schema", got, minDeviceTypes)
	}
	if got := len(schema.DeviceTypeNames); got != len(schema.DeviceTypeRevisions) {
		t.Errorf("DeviceTypeNames (%d) and DeviceTypeRevisions (%d) have different lengths", len(schema.DeviceTypeNames), len(schema.DeviceTypeRevisions))
	}
}
