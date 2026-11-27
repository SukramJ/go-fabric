// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The Access Restriction List (ARL) is not a cluster of its own: it is
// part of AccessControl (0x001F) under its MNGD (ManagedDevice) feature — the
// attributes CommissioningArl (0x0005) and Arl (0x0006) and the command
// ReviewFabricRestrictions (0x00) with its response
// ReviewFabricRestrictionsResponse (0x01) (matter.js b25ed960
// packages/model/src/standard/elements/access-control.element.ts:20, :26,
// :53, :60, :121, :131). Cluster ID 0x002B is LocalizationConfiguration
// (localization-configuration.element.ts:16-17 at db91886c).
//
// go-fabric does not implement the Managed Device use-case: the
// AccessControl server declares no MNGD feature and serves none of these
// elements.
//
// By-design entry: see notes/parity/by_design.md §ARL (C-P2-3).
const (
	// ARLClusterID was meant as the cluster ID of the Access Restriction
	// List; 0x002B is LocalizationConfiguration, and ARL has no cluster of
	// its own (see above).
	//
	// Deprecated: ARL is part of AccessControl, cluster
	// [thingfabric.dev/go-fabric/cluster/spec/accesscontrol.ClusterID]
	// (0x001F) with the MNGD feature; this constant keeps its value until
	// it goes and is removable in v0.29.0 (review II, R3-doc-3054).
	ARLClusterID uint32 = 0x002B
)
