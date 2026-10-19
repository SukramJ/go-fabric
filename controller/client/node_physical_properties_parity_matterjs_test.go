// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/NodePhysicalPropertiesTest.ts
// at 862ad75f, the case on a commissioned pair: the device's own
// specification version and device types as the client node's properties.
// The composition cases are in node_physical_properties_parity_matterjs_internal_test.go.
package client_test

import (
	"slices"
	"testing"
)

func TestNodePhysicalPropertiesPairParity(t *testing.T) {
	t.Parallel()
	t.Run("reports the specification version and no Generic Switch by default", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		p := cp.node.Properties()
		if p.SpecificationVersion == nil || *p.SpecificationVersion < 0x0103_0000 {
			t.Fatalf("specification version %v, want at least 1.3", p.SpecificationVersion)
		}
		if slices.Contains(p.DeviceTypes, 0x000F) {
			t.Fatalf("device types %v include Generic Switch", p.DeviceTypes)
		}
	})
}
