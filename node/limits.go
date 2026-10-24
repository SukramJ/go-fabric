// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// Limits are the capacity limits a host may set: the ones matter.js keeps
// configurable. Every other limit of the node is a constant, listed with its
// value on the limits page (docs/guides/limits.md). A zero field is the
// default its comment names, the value the node had before the field
// existed; [Config.Validate] refuses a value outside the field's bound.
//
// Two limits matter.js configures sit on [Config] itself, where they were
// before this struct: [Config.SupportedFabrics] and
// [Config.MaxPathsPerInvoke].
type Limits struct {
	// SubscriptionMaxInterval is the longest MaxInterval the node grants a
	// subscription: a subscriber asking for more is negotiated down to it,
	// never below its own MinIntervalFloor. Mirrors matter.js
	// network.subscriptionOptions.maxInterval (NetworkServer.ts:210,
	// applied in ServerSubscription.ts:314-344 at 862ad75f). 0 is one
	// hour (subscription.DefaultMaxIntervalCeilingSeconds, matter.js's
	// MAX_INTERVAL_PUBLISHER_LIMIT and chip's publisher limit); matter.js
	// defaults to three minutes instead. Bound: whole seconds, 1 s to 1 h
	// (MAX_INTERVAL_PUBLISHER_LIMIT caps any larger value in matter.js).
	SubscriptionMaxInterval time.Duration
	// AccessControlEntriesPerFabric is AccessControl's attribute of that
	// name and the most entries one fabric's ACL may hold; a write beyond
	// it fails with ResourceExhausted. 0 is 4
	// (mattercore.DefaultAccessControlEntriesPerFabric, matter.js
	// AccessControlServer.ts:64-66). Bound: 4 to 65534.
	AccessControlEntriesPerFabric uint16
	// SubjectsPerAccessControlEntry is AccessControl's attribute of that
	// name and the most subjects one entry may name. 0 is 4
	// (AccessControlServer.ts:58-60). Bound: 4 to 65534.
	SubjectsPerAccessControlEntry uint16
	// TargetsPerAccessControlEntry is AccessControl's attribute of that
	// name and the most targets one entry may name. 0 is 4
	// (AccessControlServer.ts:61-63). Bound: 3 to 65534.
	TargetsPerAccessControlEntry uint16
	// MaxGroupsPerFabric is GroupKeyManagement's attribute of that name and
	// the most groups one fabric may hold; a group beyond it is refused
	// with ResourceExhausted. 0 is 22 (mattercore.DefaultMaxGroupsPerFabric,
	// matter.js GroupKeyManagementServer.ts:610). Bound: the attribute's
	// uint16 range.
	MaxGroupsPerFabric uint16
	// MaxGroupKeysPerFabric is GroupKeyManagement's attribute of that name
	// and the most group key sets one fabric may hold; KeySetWrite beyond
	// it fails with ResourceExhausted. 0 is 20
	// (mattercore.DefaultMaxGroupKeysPerFabric,
	// GroupKeyManagementServer.ts:609). Bound: 1 to 65535.
	MaxGroupKeysPerFabric uint16
}

// Access-control capacity bounds (Matter, access-control.element.ts:38-49
// at 862ad75f).
const (
	minAccessControlEntriesPerFabric = 4
	minSubjectsPerAccessControlEntry = 4
	minTargetsPerAccessControlEntry  = 3
	maxAccessControlCapacity         = 65534
)

// validate returns one error per field outside its bound.
func (l Limits) validate() []error {
	var errs []error
	if v := l.SubscriptionMaxInterval; v != 0 {
		ceiling := time.Duration(subscription.DefaultMaxIntervalCeilingSeconds) * time.Second
		if v < time.Second || v > ceiling || v%time.Second != 0 {
			errs = append(errs, fmt.Errorf("node: Limits.SubscriptionMaxInterval %v is not whole seconds from 1s to %v", v, ceiling))
		}
	}
	for _, f := range []struct {
		name     string
		v, lower uint16
	}{
		{"AccessControlEntriesPerFabric", l.AccessControlEntriesPerFabric, minAccessControlEntriesPerFabric},
		{"SubjectsPerAccessControlEntry", l.SubjectsPerAccessControlEntry, minSubjectsPerAccessControlEntry},
		{"TargetsPerAccessControlEntry", l.TargetsPerAccessControlEntry, minTargetsPerAccessControlEntry},
	} {
		if f.v != 0 && (f.v < f.lower || f.v > maxAccessControlCapacity) {
			errs = append(errs, fmt.Errorf("node: Limits.%s %d is outside %d to %d", f.name, f.v, f.lower, maxAccessControlCapacity))
		}
	}
	return errs
}

// subscriptionConfig is the subscription engine's configuration under l.
func (l Limits) subscriptionConfig(c clock.Clock) subscription.Config {
	return subscription.Config{
		MaxIntervalCeilingSeconds: uint16(l.SubscriptionMaxInterval / time.Second), //nolint:gosec // validate bounds it to one hour
		Clock:                     c,
	}
}

// accessControl is AccessControl's capacity under l.
func (l Limits) accessControl() mattercore.AccessControlLimits {
	return mattercore.AccessControlLimits{
		EntriesPerFabric: l.AccessControlEntriesPerFabric,
		SubjectsPerEntry: l.SubjectsPerAccessControlEntry,
		TargetsPerEntry:  l.TargetsPerAccessControlEntry,
	}
}
