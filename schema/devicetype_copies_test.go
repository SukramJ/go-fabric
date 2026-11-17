// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestDeviceTypeAccessorsReturnCopies holds the device-type accessors to
// the rule AttributeFabricScope follows: what they return is the caller's
// own, so a caller's change does not alter the module's tables for the
// whole process (review II, R2-P6-api-1316).
func TestDeviceTypeAccessorsReturnCopies(t *testing.T) {
	t.Parallel()
	const dimmableLight schema.DeviceTypeID = 0x0101
	dt, ok := schema.DeviceTypeDefinitionOf(dimmableLight)
	if !ok || len(dt.Requirements) == 0 || len(dt.Requirements[0].Requirements) == 0 {
		t.Fatalf("DimmableLight: %+v, %v; want a definition with nested requirements", dt, ok)
	}
	name, nested := dt.Name, dt.Requirements[0].Requirements[0].Name
	dt.Name = "changed"
	dt.Requirements[0].Requirements[0].Name = "changed"
	dt.Requirements[0].Conformance.Op = "changed"
	again, _ := schema.DeviceTypeDefinitionOf(dimmableLight)
	if again.Name != name || again.Requirements[0].Requirements[0].Name != nested || again.Requirements[0].Conformance.Op == "changed" {
		t.Error("a change to DeviceTypeDefinitionOf's result reached the table")
	}

	base := schema.BaseDeviceTypes()
	if len(base) == 0 {
		t.Fatal("no base device type")
	}
	baseName := base[0].Name
	base[0].Name = "changed"
	base[0] = nil
	if b := schema.BaseDeviceTypes(); b[0] == nil || b[0].Name != baseName {
		t.Error("a change to BaseDeviceTypes' result reached the table")
	}

	const rootNode schema.DeviceTypeID = 0x0016
	if conds := schema.DeviceTypeConditions(rootNode); len(conds) > 0 {
		first := conds[0]
		conds[0] = "changed"
		if schema.DeviceTypeConditions(rootNode)[0] != first {
			t.Error("a change to DeviceTypeConditions' result reached the table")
		}
	} else {
		t.Fatal("RootNode declares no condition")
	}

	const onOff uint32 = 0x0006
	features := schema.ClusterFeatures(onOff)
	if len(features) == 0 {
		t.Fatal("OnOff has no feature")
	}
	first := features[0]
	features[0].Name = "changed"
	if schema.ClusterFeatures(onOff)[0] != first {
		t.Error("a change to ClusterFeatures' result reached the table")
	}
}
