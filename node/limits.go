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
// SupportedFabrics and MaxPathsPerInvoke sat on [Config] itself before
// they moved here; the Config fields are deprecated and still honoured.
type Limits struct {
	// SupportedFabrics is OperationalCredentials' SupportedFabrics, the
	// number of fabrics the node admits (Matter §11.18.4.4: 1 to 254); 0
	// is 5, the facade's value before it was configurable. matter.js
	// defaults it to 254 when unset (OperationalCredentialsServer.ts:
	// 84-88 initialize), which cluster/core does too; the facade keeps 5.
	// It replaces [Config].SupportedFabrics; both set and different fail
	// [Config.Validate].
	SupportedFabrics uint8
	// MaxPathsPerInvoke is the most commands one InvokeRequest may carry:
	// the root BasicInformation advertises it, the PASE and CASE session
	// parameters carry it and the invoke path refuses a larger batch with
	// InvalidAction. 0 is im.DefaultMaxPathsPerInvoke (10), matter.js's
	// DEFAULT_MAX_PATHS_PER_INVOKE. Mirrors matter.js
	// basicInformation.maxPathsPerInvoke, which InteractionServer.ts:198
	// enforces. It replaces [Config].MaxPathsPerInvoke; both set and
	// different fail [Config.Validate].
	MaxPathsPerInvoke uint16
	// SubscriptionMaxInterval is the longest MaxInterval the node grants a
	// subscription: a subscriber asking for more is negotiated down to it,
	// never below its own MinIntervalFloor. Mirrors matter.js
	// network.subscriptionOptions.maxInterval (NetworkServer.ts:210,
	// applied in ServerSubscription.ts:314-344 at db021da0). 0 is one
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
	// and the most group key sets one fabric may hold, the IPK's implicit
	// key set 0 counted as one (GroupKeyManagementServer.ts:379-380);
	// KeySetWrite beyond it fails with ResourceExhausted. 0 is 20
	// (mattercore.DefaultMaxGroupKeysPerFabric,
	// GroupKeyManagementServer.ts:609). Bound: 1 to 65535.
	MaxGroupKeysPerFabric uint16
	// BDXAdditionalMrpDelay is the margin the node adds to a peer's MRP
	// base interval on the exchange of every inbound BDX transfer its BDX
	// responder accepts (an OTA requestor's image download); the backoff
	// amplifies it. Mirrors matter.js LimitsConfig.bdxAdditionalMrpDelay
	// (NetworkServer.ts:152-153 at db021da0), the BDX class of the network
	// profile's MRP margins (NetworkProfile.ts:156-159), applied as the
	// exchange's peerAdditionalMrpDelay (BdxProtocol.ts:139-142). 0 adds
	// none. Bound: not negative (SoftwareUpdateManager.ts:60-67,
	// assertTransferOptions).
	BDXAdditionalMrpDelay time.Duration
}

// Access-control capacity bounds (Matter, access-control.element.ts:38-49
// at db021da0).
const (
	minAccessControlEntriesPerFabric = 4
	minSubjectsPerAccessControlEntry = 4
	minTargetsPerAccessControlEntry  = 3
	maxAccessControlCapacity         = 65534
)

// foldLimits makes the deprecated Config.SupportedFabrics and
// Config.MaxPathsPerInvoke and their Limits fields one value: the Limits
// field when set, else the Config field, written to both (Validate
// refuses the two set and different).
func (c *Config) foldLimits() {
	if c.Limits.SupportedFabrics != 0 {
		c.SupportedFabrics = c.Limits.SupportedFabrics //nolint:staticcheck // the deprecated field is still honoured
	}
	c.Limits.SupportedFabrics = c.SupportedFabrics //nolint:staticcheck // as above
	if c.Limits.MaxPathsPerInvoke != 0 {
		c.MaxPathsPerInvoke = c.Limits.MaxPathsPerInvoke //nolint:staticcheck // as above
	}
	c.Limits.MaxPathsPerInvoke = c.MaxPathsPerInvoke //nolint:staticcheck // as above
}

// limitsPairErrors refuses a deprecated Config limit and its Limits field
// both set and different: one of them would be dropped.
func (c *Config) limitsPairErrors() []error {
	var errs []error
	if a, b := c.SupportedFabrics, c.Limits.SupportedFabrics; a != 0 && b != 0 && a != b { //nolint:staticcheck // the deprecated field is still honoured
		errs = append(errs, fmt.Errorf("node: Config.SupportedFabrics %d and Limits.SupportedFabrics %d differ; set Limits.SupportedFabrics only", a, b))
	}
	if a, b := c.MaxPathsPerInvoke, c.Limits.MaxPathsPerInvoke; a != 0 && b != 0 && a != b { //nolint:staticcheck // as above
		errs = append(errs, fmt.Errorf("node: Config.MaxPathsPerInvoke %d and Limits.MaxPathsPerInvoke %d differ; set Limits.MaxPathsPerInvoke only", a, b))
	}
	return errs
}

// validate returns one error per field outside its bound.
func (l Limits) validate() []error {
	var errs []error
	if v := l.SubscriptionMaxInterval; v != 0 {
		ceiling := time.Duration(subscription.DefaultMaxIntervalCeilingSeconds) * time.Second
		if v < time.Second || v > ceiling || v%time.Second != 0 {
			errs = append(errs, fmt.Errorf("node: Limits.SubscriptionMaxInterval %v is not whole seconds from 1s to %v", v, ceiling))
		}
	}
	if l.BDXAdditionalMrpDelay < 0 {
		errs = append(errs, fmt.Errorf("node: Limits.BDXAdditionalMrpDelay %v is negative", l.BDXAdditionalMrpDelay))
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
