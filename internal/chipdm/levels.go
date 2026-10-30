// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/script/levelgen/leveltable"
)

// The specification-level cross-check (ADR 0015 addendum 2026-10-30,
// brief S3 part 5.3): the level files (parity/levels/<level>.json,
// extracted from matter.js's release tags) are compared by presence with
// CHIP's data_model/<level> — which device types, clusters, features
// (by bit), attributes, commands and events exist. Nothing of the XML is
// kept beyond the comparison; the differences go through
// LevelAcknowledged.

// LevelIDs are the ids one side states for one level.
type LevelIDs struct {
	DeviceTypes map[uint32]bool
	// Clusters maps a cluster id to its members.
	Clusters map[uint32]*LevelCluster
}

// LevelCluster are a cluster's member ids: features by bit, commands
// request and response alike.
type LevelCluster struct {
	Features, Attributes, Commands, Events map[uint32]bool
}

func newLevelCluster() *LevelCluster {
	return &LevelCluster{Features: map[uint32]bool{}, Attributes: map[uint32]bool{}, Commands: map[uint32]bool{}, Events: map[uint32]bool{}}
}

// isGlobalAttribute: the global attributes (0xFFF8–0xFFFD) are compared
// as implied — the level files list them per cluster, CHIP's cluster XML
// states them once, globally.
func isGlobalAttribute(id uint32) bool { return id >= 0xFFF8 && id <= 0xFFFD }

// LevelIDsOfFile reads a level file's ids.
func LevelIDsOfFile(f leveltable.File) LevelIDs {
	out := LevelIDs{DeviceTypes: map[uint32]bool{}, Clusters: map[uint32]*LevelCluster{}}
	for _, d := range f.DeviceTypes {
		out.DeviceTypes[d.ID] = true
	}
	for ci := range f.Clusters {
		c := &f.Clusters[ci]
		lc := newLevelCluster()
		for _, ft := range c.Features {
			lc.Features[uint32(ft.Bit)] = true
		}
		for _, id := range c.Attributes {
			if !isGlobalAttribute(id) {
				lc.Attributes[id] = true
			}
		}
		for _, id := range c.Commands {
			lc.Commands[id] = true
		}
		for _, id := range c.Events {
			lc.Events[id] = true
		}
		out.Clusters[c.ID] = lc
	}
	return out
}

// LevelIDsOfModel reads CHIP's ids, a derived cluster's with what it
// inherits (the comparison's own inheritance, compare.go effective). An
// element without an id (a feature without a bit) is not counted.
func LevelIDsOfModel(m *Model) LevelIDs {
	c := &comparer{chip: m, chipBases: map[string]*Cluster{}}
	for _, cl := range slices.Concat(m.Clusters, m.BaseClusters) {
		c.chipBases[canonicalName(cl.Name)] = cl
	}
	out := LevelIDs{DeviceTypes: map[uint32]bool{}, Clusters: map[uint32]*LevelCluster{}}
	for _, d := range m.DeviceTypes {
		if d.ID != nil {
			out.DeviceTypes[*d.ID] = true
		}
	}
	for _, cl := range m.Clusters {
		if cl.ID == nil {
			continue
		}
		eff := c.effective(cl)
		lc := newLevelCluster()
		for _, f := range eff.features {
			if bit, err := strconv.ParseUint(f.Constraint, 10, 32); err == nil {
				lc.Features[uint32(bit)] = true
			}
		}
		for _, e := range eff.attributes {
			if e.ID != nil && !isGlobalAttribute(*e.ID) {
				lc.Attributes[*e.ID] = true
			}
		}
		for _, e := range eff.commands {
			if e.ID != nil {
				lc.Commands[*e.ID] = true
			}
		}
		for _, e := range eff.events {
			if e.ID != nil {
				lc.Events[*e.ID] = true
			}
		}
		out.Clusters[*cl.ID] = lc
	}
	return out
}

// LevelSegment is a level's first path segment ("L1_4_2"): a level's dots
// would split the path.
func LevelSegment(level string) string { return "L" + strings.ReplaceAll(level, ".", "_") }

// presence values of a level difference.
const (
	present = "present"
	absent  = "absent"
)

// CompareLevel compares the presence of every id of ours (the level file)
// and chip (data_model/<level>), both ways. A difference's path is
// "<LevelSegment>.deviceType.0x<id>", "<…>.cluster.0x<id>" or
// "<…>.cluster.0x<id>.<feature|attribute|command|event>.<bit or 0x id>";
// its property "presence", its values "present" or "absent". Members are
// compared only on clusters both sides state.
func CompareLevel(level string, ours, chip LevelIDs) []Difference {
	seg := LevelSegment(level)
	var out []Difference
	diff := func(path string, inChip, inOurs bool) {
		if inChip == inOurs {
			return
		}
		d := Difference{Path: seg + "." + path, Property: "presence", Chip: absent, Ours: absent}
		if inChip {
			d.Chip = present
		}
		if inOurs {
			d.Ours = present
		}
		out = append(out, d)
	}
	for _, id := range unionIDs(ours.DeviceTypes, chip.DeviceTypes) {
		diff(fmt.Sprintf("deviceType.0x%04x", id), chip.DeviceTypes[id], ours.DeviceTypes[id])
	}
	ids := map[uint32]bool{}
	for id := range ours.Clusters {
		ids[id] = true
	}
	for id := range chip.Clusters {
		ids[id] = true
	}
	for _, id := range sortedIDSet(ids) {
		o, c := ours.Clusters[id], chip.Clusters[id]
		path := fmt.Sprintf("cluster.0x%04x", id)
		if o == nil || c == nil {
			diff(path, c != nil, o != nil)
			continue
		}
		for _, m := range []struct {
			kind      string
			ours, chp map[uint32]bool
			format    string
		}{
			{"feature", o.Features, c.Features, "%d"},
			{"attribute", o.Attributes, c.Attributes, "0x%04x"},
			{"command", o.Commands, c.Commands, "0x%02x"},
			{"event", o.Events, c.Events, "0x%02x"},
		} {
			for _, x := range unionIDs(m.ours, m.chp) {
				diff(path+"."+m.kind+"."+fmt.Sprintf(m.format, x), m.chp[x], m.ours[x])
			}
		}
	}
	slices.SortFunc(out, func(a, b Difference) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func unionIDs(a, b map[uint32]bool) []uint32 {
	all := map[uint32]bool{}
	for k := range a {
		all[k] = true
	}
	for k := range b {
		all[k] = true
	}
	return sortedIDSet(all)
}

func sortedIDSet(m map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
