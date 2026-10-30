// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import (
	"math/bits"
	"slices"
)

// Level is a specification level: a Matter revision matter.js has shipped
// a release for ("1.4", "1.4.1", "1.4.2", "1.5.1", "1.6.0", "1.6.1"). The
// table of what exists at each level (levels_gen.go) is generated from
// matter.js's tagged releases (parity/levels/<level>.json, concept v11
// §C.5, ADR 0030 decision 5), never from connectedhomeip's data model XML.
type Level string

// MaxLevels is how many levels a [LevelSet] holds.
const MaxLevels = 8

// Levels returns the levels, ascending; the last is the pin's revision
// (parity/schema.json matter.revision).
func Levels() []Level { return slices.Clone(levels) }

// LevelSet is a set of levels: bit i is Levels()[i]. A set, not a "since"
// revision: an element's history is not monotonic (Thermostat's SCH
// feature is in the 1.4.2 model and not in the 1.5.1 one).
type LevelSet uint8

// Has reports whether l is in s; false for a level Levels does not list.
func (s LevelSet) Has(l Level) bool {
	i := slices.Index(levels, l)
	return i >= 0 && s&(1<<i) != 0
}

// Levels returns the levels of s, ascending.
func (s LevelSet) Levels() []Level {
	var out []Level
	for x := s; x != 0; x &= x - 1 {
		if i := bits.TrailingZeros8(uint8(x)); i < len(levels) {
			out = append(out, levels[i])
		}
	}
	return out
}

// levelCluster is one cluster of the level table.
type levelCluster struct {
	levels     LevelSet
	revisions  [MaxLevels]uint16
	features   map[uint8]LevelSet
	attributes map[uint32]LevelSet
	commands   map[uint32]LevelSet
	events     map[uint32]LevelSet
}

// DeviceTypeLevels returns the levels the device type exists at; 0 for
// one no level knows.
func DeviceTypeLevels(id DeviceTypeID) LevelSet { return deviceTypeLevels[id] }

// ClusterLevels returns the levels the cluster exists at.
func ClusterLevels(id uint32) LevelSet {
	if c := clusterLevels[id]; c != nil {
		return c.levels
	}
	return 0
}

// ClusterFeatureLevels returns the levels the cluster's FeatureMap bit
// (its index, not its mask) exists at.
func ClusterFeatureLevels(cluster uint32, bit uint8) LevelSet {
	if c := clusterLevels[cluster]; c != nil {
		return c.features[bit]
	}
	return 0
}

// ClusterAttributeLevels returns the levels the cluster's attribute
// exists at; the global attributes are the cluster's own.
func ClusterAttributeLevels(cluster, attr uint32) LevelSet {
	if c := clusterLevels[cluster]; c != nil {
		return c.attributes[attr]
	}
	return 0
}

// ClusterCommandLevels returns the levels the cluster's command id exists
// at, a request or a response.
func ClusterCommandLevels(cluster, cmd uint32) LevelSet {
	if c := clusterLevels[cluster]; c != nil {
		return c.commands[cmd]
	}
	return 0
}

// ClusterEventLevels returns the levels the cluster's event exists at.
func ClusterEventLevels(cluster, ev uint32) LevelSet {
	if c := clusterLevels[cluster]; c != nil {
		return c.events[ev]
	}
	return 0
}

// ClusterRevisionAt returns the cluster's ClusterRevision in the model of
// level l; false when the cluster or the level is unknown there. The
// served revision is always the pin's (concept v11, out of scope).
func ClusterRevisionAt(cluster uint32, l Level) (uint16, bool) {
	c := clusterLevels[cluster]
	i := slices.Index(levels, l)
	if c == nil || i < 0 || c.revisions[i] == 0 {
		return 0, false
	}
	return c.revisions[i], true
}
