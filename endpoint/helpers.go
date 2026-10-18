// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"errors"

	"github.com/SukramJ/go-fabric/schema"
)

// listedDeviceTypes returns the device-type ids ep's Descriptor
// DeviceTypeList carries, in its order: a part lists its own device type
// alone, a bridged endpoint its primary device type (when it has one) and
// BridgedNode. [ClusterServers] builds the list from it, and the assembly
// reads its composition from it, so the two cannot disagree.
func listedDeviceTypes(ep *Endpoint) []uint16 {
	const bridgedNode = uint16(matterDeviceTypeBridgedNode)
	if ep.Part {
		return []uint16{ep.DeviceType}
	}
	if ep.DeviceType == 0 {
		return []uint16{bridgedNode}
	}
	return []uint16{ep.DeviceType, bridgedNode}
}

// composesFullFamily reports whether one of ep's listed device types
// composes the endpoint's PartsList of every descendant rather than of its
// own children (Core § 9.2.7), read from the schema at run time.
//
// Mirrors matter.js packages/node/src/behaviors/descriptor/DescriptorServer.ts:262-270
// (#composesFullFamily: some entry of the DeviceTypeList whose
// effectiveComposition is EndpointComposition.FullFamily).
func composesFullFamily(ep *Endpoint) bool {
	for _, id := range listedDeviceTypes(ep) {
		if dt, ok := schema.DeviceTypeDefinitionOf(uint32(id)); ok && dt.Composition == schema.CompositionFullFamily {
			return true
		}
	}
	return false
}

// isNotFound matches the [ErrNotFound] sentinel via errors.Is. Wrapped
// to keep the assembler call sites concise.
func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// deviceTypeRevision returns the Matter Application Cluster Library
// revision the bridge advertises in `Descriptor.DeviceTypeList` for
// the supplied primary device-type ID.
//
// The general case delegates to [schema.DeviceTypeRevision], which is
// codegen'd from parity/schema.json via
// `go generate ./schema/...`. This guarantees that matter.js HEAD
// updates propagate automatically on the next codegen run without
// requiring manual edits here.
//
// A small customDeviceTypeRevision fallback handles bridge-specific
// overrides — currently only the Apple-Home RootNode bypass documented
// in notes/parity/by_design.md task #66. Unknown device-type IDs fall
// back to revision 1 so the function never breaks an exposure for a
// custom or vendor-specific type the schema does not yet cover.
//
// Why this matters: Apple Home's HAP service mapper validates the
// (deviceType, revision) tuple against an internal whitelist; a
// revision mismatch surfaces as "Attribute report <private> is not
// parsed into a known struct" and aborts HAP service rebuild with
// HAPErrorDomain Code=14 ("No Endpoints In Use at endpoint 0").
// Matter §1.4 guarantees strict-superset semantics — revisions never
// shrink — so a too-high value is also rejected by mappers with
// per-version schema tables.
func deviceTypeRevision(deviceType uint16) uint16 {
	// Bridge-specific overrides — device types where we deliberately
	// diverge from matter.js HEAD. Each entry must reference the
	// notes/parity/by_design.md task that documents the divergence.
	if rev, ok := customDeviceTypeRevision(deviceType); ok {
		return rev
	}
	// General case: codegen'd from matter.js HEAD schema snapshot.
	// Mirrors matter.js packages/node/src/devices/<name>.ts revision fields.
	if rev, ok := schema.DeviceTypeRevision(uint32(deviceType)); ok {
		return rev
	}
	return 1
}

// customDeviceTypeRevision contains bridge-specific device-type revision
// overrides that deliberately diverge from matter.js HEAD. Each entry is
// documented in notes/parity/by_design.md.
func customDeviceTypeRevision(_ uint16) (uint16, bool) {
	// No active overrides at this time.
	// Pattern for future additions:
	//   case 0x0016: // RootNode — Apple-Home bypass (notes/parity/by_design.md)
	//       return 3, true
	return 0, false
}
