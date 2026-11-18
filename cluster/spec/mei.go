// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

// VendorPrefix is the vendor prefix of a Manufacturer Extensible
// Identifier — a cluster, attribute, command or event id: its upper 16
// bits, the split [CheckClusterID] and its siblings make. 0 names a
// standard element; any other value the vendor whose manufacturer-specific
// element the id is (review II, F-db021da0-55).
func VendorPrefix(mei uint32) uint16 { return uint16(mei >> 16) } //nolint:gosec // the upper half of a 32-bit id
