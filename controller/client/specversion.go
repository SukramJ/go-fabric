// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import "fmt"

// SpecVersion is a BasicInformation SpecificationVersion decoded into its
// four component bytes, most significant first: 0x01040200 is version
// 1.4.2, Reserved 0 — the layout matter.js documents for the attribute
// (packages/types/src/clusters/basic-information.d.ts:168-200 at
// 862ad75f; Specification.SPECIFICATION_VERSION 0x01060100 is 1.6.1,
// packages/model/src/common/Specification.ts:110). Compare versions by
// [SpecVersion.Uint32], over all 32 bits, as the specification says.
type SpecVersion struct {
	Major, Minor, Patch, Reserved uint8
}

// SpecVersionOf decodes a SpecificationVersion attribute value.
func SpecVersionOf(v uint32) SpecVersion {
	return SpecVersion{Major: uint8(v >> 24), Minor: uint8(v >> 16), Patch: uint8(v >> 8), Reserved: uint8(v)} //nolint:gosec // byte extraction
}

// Uint32 is the attribute value of v.
func (v SpecVersion) Uint32() uint32 {
	return uint32(v.Major)<<24 | uint32(v.Minor)<<16 | uint32(v.Patch)<<8 | uint32(v.Reserved)
}

// String is "Major.Minor.Patch" ("1.4.0"), with ".Reserved" appended when
// that byte is not zero.
func (v SpecVersion) String() string {
	if v.Reserved != 0 {
		return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Patch, v.Reserved)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Specification is the node's specification version, decoded from
// [Properties].SpecificationVersion; false while it is unknown or zero
// (absent before BasicInformation revision 3, the specification says).
func (p Properties) Specification() (SpecVersion, bool) {
	if p.SpecificationVersion == nil || *p.SpecificationVersion == 0 {
		return SpecVersion{}, false
	}
	return SpecVersionOf(*p.SpecificationVersion), true
}
