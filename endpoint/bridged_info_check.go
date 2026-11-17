// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
)

// checkBridgedInfo holds what a [Spec] states of its bridged device's
// BridgedDeviceBasicInformation — the stated Patch, and the VendorName the
// endpoint serves (Spec.VendorName, else the bridge's) — to the generated
// definition's constraint, the check a controller's write of the
// attribute meets (spec.CheckWritten: a string's length in UTF-16 code
// units, spec.StringLength, and its codepoints against cpMax). A value
// beyond it refuses the assembly, naming the endpoint and the attribute:
// the assembler serves a stated value as stated, never cut (R2-P6-wire-2502,
// decision 201). device.New refuses the same values earlier, so a device
// of package device never reaches this refusal.
func checkBridgedInfo(s *Spec, bridgeVendorName string) error {
	key := "<no key>"
	if s.StableKey != nil {
		key = s.StableKey.String()
	}
	values := s.BridgedBasicInformation.AttributeValues()
	if _, stated := values[bdbidef.AttrVendorName]; !stated {
		if name := cmp.Or(s.VendorName, bridgeVendorName); name != "" {
			values[bdbidef.AttrVendorName] = name
		}
	}
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if err := spec.CheckWritten(bdbidef.Definition, 0, id, values[id], nil); err != nil {
			return fmt.Errorf("endpoint: %s: BridgedDeviceBasicInformation %s (0x%04X): %w",
				key, bdbidef.Definition.Attribute(id).Name, id, err)
		}
	}
	return nil
}
