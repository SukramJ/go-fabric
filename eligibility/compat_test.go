// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package eligibility

import (
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestCompatDeviceTypesFromSchema: the device types Compat names are the
// schema snapshot's (W5-api-13), not hand-kept ids.
func TestCompatDeviceTypesFromSchema(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]uint16{"WaterValve": deviceTypeWaterValve, "WaterLeakDetector": deviceTypeWaterLeakDetector} {
		if got, ok := schema.DeviceTypeName(schema.DeviceTypeID(id)); !ok || got != name {
			t.Errorf("0x%04X is %q in the schema, want %s", id, got, name)
		}
	}
}
