// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"context"
	"errors"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// SchedulesClusterID is the Schedules cluster ID per Matter Application
// Cluster Specification §11.20.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
const SchedulesClusterID uint32 = 0x0024

// Schedules cluster attribute IDs per Matter §11.20.5.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
const (
	SchedulesAttrScheduleProgrammingHandle         uint32 = 0x0000
	SchedulesAttrNumberOfSchedules                 uint32 = 0x0001
	SchedulesAttrNumberOfScheduleTransitions       uint32 = 0x0002
	SchedulesAttrNumberOfScheduleTransitionsPerDay uint32 = 0x0003
	SchedulesAttrNextScheduleHandle                uint32 = 0x0004
	SchedulesAttrSchedules                         uint32 = 0x0005
	SchedulesAttrFeatureMap                        uint32 = 0xFFFC
	SchedulesAttrClusterRevision                   uint32 = 0xFFFD
)

// SchedulesClusterRevision is the Matter cluster revision for Schedules
// (0x0024). Cluster revision 1 covers the initial Matter 1.3 publication.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
const SchedulesClusterRevision uint16 = 1

// ErrSchedulesReadOnly is returned when a write or invoke is attempted on
// the Schedules cluster: the host owns schedule mutation, and controllers
// read the schedules only.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
var ErrSchedulesReadOnly = errors.New("wire: Schedules cluster is read-only")

// ScheduleEntry is the bridge-side representation of one programmed
// setpoint transition. [SchedulesSource.MatterScheduleEntries] returns one
// entry per programmed slot; the cluster groups them by DayOfWeek when
// assembling the Schedules attribute array.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
type ScheduleEntry struct {
	// DayOfWeek is the day index using the Matter convention: 0=Sunday,
	// 1=Monday, …, 6=Saturday.
	DayOfWeek uint8
	// TransitionTime is the minutes-since-midnight for this transition.
	TransitionTime uint16
	// Setpoint is the target temperature in °C.
	Setpoint float64
}

// ScheduleStruct is a single entry in the Schedules attribute array,
// collecting all transitions for one day slot.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
type ScheduleStruct struct {
	// DayOfWeek is the Matter-convention day index (0=Sun..6=Sat).
	DayOfWeek uint8
	// Transitions holds the ordered setpoint transitions for this day.
	Transitions []ScheduleEntry
}

// SchedulesSource is the read interface implemented by a host's model that
// exposes a week-program schedule. The Schedules cluster server calls
// MatterScheduleEntries to build the attribute snapshot; it does not poll —
// reads are always on-demand at attribute-read time.
//
// The implementing type maps its own week-program representation to the
// flat []ScheduleEntry slice. The cluster server then groups entries by
// DayOfWeek to form ScheduleStruct values in the Schedules attribute.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
type SchedulesSource interface {
	MatterScheduleEntries() []ScheduleEntry
}

// SchedulesServer is a read-only [contract.ClusterServer] projection
// of a [SchedulesSource] onto the Matter Schedules cluster (0x0024).
//
// Write and invoke are rejected with [ErrSchedulesReadOnly]: the host
// manages its week programs; controllers can read the current schedule but
// cannot modify it through this cluster.
//
// Grouping: The cluster groups [ScheduleEntry] values by DayOfWeek into
// [ScheduleStruct] values for the Schedules attribute. The NumberOfSchedules
// attribute counts the distinct day groups; NumberOfScheduleTransitions counts
// total entries; NumberOfScheduleTransitionsPerDay counts the maximum
// transitions in any single day group.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
type SchedulesServer struct {
	src SchedulesSource
}

// NewSchedulesServer constructs a [SchedulesServer] backed by src. src must
// not be nil.
//
// Deprecated: the Schedules cluster (0x0024) is not in the Matter model
// this module serves (parity/schema.json has no such cluster) and nothing
// mounts this surface; a thermostat's week programs are the Thermostat
// server's schedules (cluster/thermo, docs/guides/thermostat.md).
// Removable in v0.25.0.
func NewSchedulesServer(src SchedulesSource) *SchedulesServer {
	return &SchedulesServer{src: src}
}

// Compile-time assertions: SchedulesServer satisfies MatterClusterServer
// and the attribute-lister capability.
var (
	_ contract.ClusterServer          = (*SchedulesServer)(nil)
	_ contract.ClusterAttributeLister = (*SchedulesServer)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (s *SchedulesServer) MatterClusterID() uint32 { return SchedulesClusterID }

// MatterRead implements [contract.ClusterServer]. It resolves the
// requested attribute from the current schedule snapshot returned by the
// source. Returns (nil, false) only for optional attributes that carry no
// meaningful value (ScheduleProgrammingHandle, NextScheduleHandle) per the
// Matter spec's nullable-attribute rules.
func (s *SchedulesServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case SchedulesAttrScheduleProgrammingHandle:
		// Optional attribute; this bridge does not assign programming handles.
		return nil, false
	case SchedulesAttrNextScheduleHandle:
		// Optional attribute; the host drives schedule activation, not Matter.
		return nil, false
	case SchedulesAttrNumberOfSchedules:
		groups := groupByDay(s.src.MatterScheduleEntries())
		return uint8(len(groups)), true //nolint:gosec // count bounded by number of week days (≤7); see #20
	case SchedulesAttrNumberOfScheduleTransitions:
		entries := s.src.MatterScheduleEntries()
		return uint8(len(entries)), true //nolint:gosec // wraps past 255 entries: the stub is deprecated and kept as it is for its window (R2-P6-api-1215)
	case SchedulesAttrNumberOfScheduleTransitionsPerDay:
		groups := groupByDay(s.src.MatterScheduleEntries())
		return uint8(maxPerDay(groups)), true //nolint:gosec // wraps past 255 entries: the stub is deprecated and kept as it is for its window (R2-P6-api-1215)
	case SchedulesAttrSchedules:
		groups := groupByDay(s.src.MatterScheduleEntries())
		if len(groups) == 0 {
			// No schedule data yet — the bridge maps (nil, false) to a
			// Matter NULL / stale-data status response, matching the
			// convention used by temperature and humidity measurement
			// clusters when no observation has arrived.
			return nil, false
		}
		return groups, true
	case SchedulesAttrFeatureMap:
		// No optional Matter Schedules features are enabled.
		return uint32(0), true
	case SchedulesAttrClusterRevision:
		return SchedulesClusterRevision, true
	default:
		return nil, false
	}
}

// MatterWrite implements [contract.ClusterServer]. All writes are
// rejected: the Schedules cluster is read-only here.
func (s *SchedulesServer) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return spec.Errorf(im.StatusUnsupportedWrite, "%w: attribute 0x%04X", ErrSchedulesReadOnly, attrID)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// server stores no written value, so no write changes it.
func (s *SchedulesServer) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return false, s.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer]. All invocations
// are rejected: the Schedules cluster exposes no commands here.
func (s *SchedulesServer) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, spec.Errorf(im.StatusUnsupportedCommand, "%w: command 0x%02X", ErrSchedulesReadOnly, cmdID)
}

// MatterReportable implements [contract.ClusterServer]. Only the
// Schedules array attribute is reportable; the counter attributes are
// derived from the same source and would change in lockstep.
func (s *SchedulesServer) MatterReportable() []uint32 {
	return []uint32{SchedulesAttrSchedules}
}

// MatterAttributes lists every Schedules (0x0024) attribute the server
// implements via MatterRead. Apple Home's HAP service rebuild reads the
// full attribute set; without this the dispatcher falls back to
// MatterReportable's single attribute.
func (s *SchedulesServer) MatterAttributes() []uint32 {
	return []uint32{
		SchedulesAttrNumberOfSchedules,
		SchedulesAttrNumberOfScheduleTransitions,
		SchedulesAttrNumberOfScheduleTransitionsPerDay,
		SchedulesAttrSchedules,
	}
}

// groupByDay aggregates a flat slice of [ScheduleEntry] values into a slice
// of [ScheduleStruct], one per distinct DayOfWeek, ordered by first
// appearance. Within each group, transitions retain their input order.
func groupByDay(entries []ScheduleEntry) []ScheduleStruct {
	if len(entries) == 0 {
		return nil
	}
	// Use ordered insertion to keep day order deterministic.
	idx := make(map[uint8]int)
	var groups []ScheduleStruct
	for _, e := range entries {
		i, ok := idx[e.DayOfWeek]
		if !ok {
			i = len(groups)
			idx[e.DayOfWeek] = i
			groups = append(groups, ScheduleStruct{DayOfWeek: e.DayOfWeek})
		}
		groups[i].Transitions = append(groups[i].Transitions, e)
	}
	return groups
}

// maxPerDay returns the maximum number of transitions in any single
// [ScheduleStruct]. Returns 0 for an empty slice.
func maxPerDay(groups []ScheduleStruct) int {
	best := 0
	for _, g := range groups {
		if n := len(g.Transitions); n > best {
			best = n
		}
	}
	return best
}
