// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/contract"

// ClusterDeclaration is one cluster's declaration on a device in the form
// the device kit applies it: what a generated <cluster>def.Declare states,
// untyped, and what the kit's per-cluster options (device.WithFeatures,
// WithInitial, WithElements, WithServer) state. The kit merges the
// declarations of one cluster into one; it is the kit's form, not one a
// host writes.
//
// The elements a declaration serves follow matter.js's rule: an optional
// attribute with a value is supported, an optional command with an
// implementation is (packages/node/src/behavior/cluster/ValidatedElements.ts:251-252,
// :270-278 for attributes; :301-318 for commands).
type ClusterDeclaration struct {
	// Cluster is the cluster id.
	Cluster uint32
	// Features is the FeatureMap selection, when HasFeatures; without it
	// the device type's.
	Features    uint32
	HasFeatures bool
	// Initial are attribute values the server starts with, over the
	// device type's defaults; each optional attribute among them is
	// served.
	Initial map[uint32]any
	// Elements are optional attributes, commands and events served beyond
	// the ones the device type mandates (Features unused).
	Elements Options
	// Server is the host's own server for the cluster, in place of the
	// kit's default.
	Server contract.ClusterServer
	// Transitions runs the server's commands as transitions over time
	// (LevelControl, ColorControl; ADR 0014), where false applies the
	// target at once.
	Transitions bool
	// Attach installs the declaration's hooks — the write veto and the
	// command handlers — on the server once it is built; nil installs
	// none.
	Attach func(srv contract.ClusterServer) error
	// Ports is the typed declaration the host ports are read from by the
	// kit's default server for the cluster; nil states none.
	Ports any
}
