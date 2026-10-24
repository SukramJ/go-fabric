// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The capacity limits a host may set on the root's AccessControl and
// GroupKeyManagement servers, and the defaults a zero value takes. matter.js
// keeps each of them as server state the node's configuration may set
// (AccessControlServer.ts:56-66 initialize,
// GroupKeyManagementServer.ts:609-610 at 862ad75f); the constants below are
// those defaults.
const (
	// DefaultAccessControlEntriesPerFabric is AccessControl's
	// AccessControlEntriesPerFabric when unset (matter.js
	// AccessControlServer.ts:64-66).
	DefaultAccessControlEntriesPerFabric = accessControlEntriesPerFabricLimit
	// DefaultSubjectsPerAccessControlEntry is AccessControl's
	// SubjectsPerAccessControlEntry when unset (AccessControlServer.ts:58-60).
	DefaultSubjectsPerAccessControlEntry = accessControlSubjectsPerEntry
	// DefaultTargetsPerAccessControlEntry is AccessControl's
	// TargetsPerAccessControlEntry when unset (AccessControlServer.ts:61-63).
	DefaultTargetsPerAccessControlEntry = accessControlTargetsPerEntry
	// DefaultMaxGroupsPerFabric is GroupKeyManagement's MaxGroupsPerFabric
	// when [GroupKeyMgmtConfig] leaves it 0 (GroupKeyManagementServer.ts:610).
	DefaultMaxGroupsPerFabric = defaultMaxGroupsPerFabric
	// DefaultMaxGroupKeysPerFabric is GroupKeyManagement's
	// MaxGroupKeysPerFabric when [GroupKeyMgmtConfig] leaves it 0
	// (GroupKeyManagementServer.ts:609).
	DefaultMaxGroupKeysPerFabric = defaultMaxGroupKeysPerFabric
)

// AccessControlLimits are AccessControl's three capacity attributes, which
// are also the limits an ACL write is held to (ResourceExhausted beyond
// them, AccessControlServer.ts:179-198). A zero field is its default.
type AccessControlLimits struct {
	// EntriesPerFabric is AccessControlEntriesPerFabric (Matter: 4 to 65534).
	EntriesPerFabric uint16
	// SubjectsPerEntry is SubjectsPerAccessControlEntry (4 to 65534).
	SubjectsPerEntry uint16
	// TargetsPerEntry is TargetsPerAccessControlEntry (3 to 65534;
	// the default is 4, as matter.js's).
	TargetsPerEntry uint16
}

// SetLimits sets the capacity attributes and the limits an ACL write is held
// to. Call it before the node serves; the attributes are fixed (quality F)
// and a later change would advertise one capacity and enforce another.
func (a *AccessControl) SetLimits(l AccessControlLimits) { a.limits = l }

func (a *AccessControl) entriesPerFabric() uint16 {
	return orUint16(a.limits.EntriesPerFabric, accessControlEntriesPerFabricLimit)
}

func (a *AccessControl) subjectsPerEntry() uint16 {
	return orUint16(a.limits.SubjectsPerEntry, accessControlSubjectsPerEntry)
}

func (a *AccessControl) targetsPerEntry() uint16 {
	return orUint16(a.limits.TargetsPerEntry, accessControlTargetsPerEntry)
}

func orUint16(v, def uint16) uint16 {
	if v == 0 {
		return def
	}
	return v
}
