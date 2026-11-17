// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
)

// checkBridgedInfo holds what a [Spec] that serves BridgedDeviceBasicInformation
// states of its bridged device — the stated Patch and its own VendorName —
// to the generated definition's constraint, the check a controller's write
// of the attribute meets (spec.CheckWritten: a string's length in UTF-16
// code units, spec.StringLength, and its codepoints against cpMax). A value
// beyond it refuses the assembly, naming the endpoint and the attribute:
// the assembler serves a stated value as stated, never cut (R2-P6-wire-2502,
// decision 201). device.New refuses the same values earlier, so a device
// of package device never reaches this refusal. A part, or a Spec that
// serves nothing ([ClusterServers]), serves no BridgedDeviceBasicInformation
// and is not checked. The bridge's own VendorName, the fallback a bridged
// endpoint serves without one of its own, is checked once, by
// [Config.Validate].
func checkBridgedInfo(s *Spec, part bool) error {
	if !servesBridgedInfo(s, part) {
		return nil
	}
	key := "<no key>"
	if s.StableKey != nil {
		key = s.StableKey.String()
	}
	values := s.BridgedBasicInformation.AttributeValues()
	if _, stated := values[bdbidef.AttrVendorName]; !stated && s.VendorName != "" {
		values[bdbidef.AttrVendorName] = s.VendorName
	}
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if err := spec.CheckWritten(bdbidef.Definition, 0, id, values[id], nil); err != nil {
			return fmt.Errorf("endpoint: %s: BridgedDeviceBasicInformation %s (0x%04X): %w",
				key, bdbidef.Definition.Attribute(id).Name, id, err)
		}
	}
	return nil
}

// servesBridgedInfo reports whether the endpoint s assembles to serves
// BridgedDeviceBasicInformation: a bridged endpoint, not a part, with a
// cluster surface of its own (ClusterServers mounts the server after the
// source's, the measurement's or the battery's servers, and serves nothing
// without any of them).
func servesBridgedInfo(s *Spec, part bool) bool {
	return !part && (s.Source != nil || s.Measurement != nil || s.PowerSource != nil)
}
