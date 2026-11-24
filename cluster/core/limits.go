// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "fmt"

// The capacity limits a host may set on the root's AccessControl and
// GroupKeyManagement servers, and the defaults a zero value takes. matter.js
// keeps each of them as server state the node's configuration may set
// (AccessControlServer.ts:56-66 initialize,
// GroupKeyManagementServer.ts:613-614 at b25ed960); the constants below are
// those defaults.
const (
	// DefaultAccessControlEntriesPerFabric is the attribute's value when
	// unset. matter.js: accessControlEntriesPerFabric = 4
	// (AccessControlServer.ts:64-66).
	DefaultAccessControlEntriesPerFabric = accessControlEntriesPerFabricLimit
	// DefaultSubjectsPerAccessControlEntry is the attribute's value when
	// unset. matter.js: subjectsPerAccessControlEntry = 4
	// (AccessControlServer.ts:58-60).
	DefaultSubjectsPerAccessControlEntry = accessControlSubjectsPerEntry
	// DefaultTargetsPerAccessControlEntry is the attribute's value when
	// unset. matter.js: targetsPerAccessControlEntry = 4
	// (AccessControlServer.ts:61-63).
	DefaultTargetsPerAccessControlEntry = accessControlTargetsPerEntry
	// DefaultMaxGroupsPerFabric is GroupKeyManagement's MaxGroupsPerFabric
	// when [GroupKeyMgmtConfig] leaves it 0 (GroupKeyManagementServer.ts:614).
	DefaultMaxGroupsPerFabric = defaultMaxGroupsPerFabric
	// DefaultMaxGroupKeysPerFabric is GroupKeyManagement's
	// MaxGroupKeysPerFabric when [GroupKeyMgmtConfig] leaves it 0
	// (GroupKeyManagementServer.ts:613).
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

// accessControlLimitMax is the ceiling of each capacity attribute (the
// field docs of [AccessControlLimits]).
const accessControlLimitMax = 65534

// check refuses a field outside its range — EntriesPerFabric and
// SubjectsPerEntry 4 to 65534, TargetsPerEntry 3 to 65534 — with an error
// naming it; a zero field is its default and passes.
func (l AccessControlLimits) check() error {
	for _, f := range []struct {
		name         string
		value, floor uint16
	}{
		{"EntriesPerFabric", l.EntriesPerFabric, 4},
		{"SubjectsPerEntry", l.SubjectsPerEntry, 4},
		{"TargetsPerEntry", l.TargetsPerEntry, 3},
	} {
		if f.value != 0 && (f.value < f.floor || f.value > accessControlLimitMax) {
			return fmt.Errorf("core: AccessControl: Limits.%s %d is outside %d to %d", f.name, f.value, f.floor, accessControlLimitMax)
		}
	}
	return nil
}

// SetLimits sets the capacity attributes and the limits an ACL write is held
// to. Call it before the node serves; the attributes are fixed (quality F)
// and a later change would advertise one capacity and enforce another.
//
// Deprecated: pass the limits in [AccessControlConfig.Limits] to
// [NewAccessControlWith], which refuses a limit outside its range at
// construction; SetLimits assigns any value, keeps that behaviour until
// it goes and is removable in v0.29.0 (review II, R4-api-4157, decision
// 669).
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
