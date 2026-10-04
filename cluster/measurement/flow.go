// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"context"
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

// --- FlowMeasurement (0x0404) ------------------------------------------

// Flow wire range. MeasuredValue, MinMeasuredValue and MaxMeasuredValue
// are nullable uint16 (quality X, matter.js
// packages/model/src/standard/elements/flow-measurement.element.ts:15-27),
// so 0xFFFF is the null sentinel and 65534 is the largest value the wire
// can carry. MinMeasuredValue is constrained "max 65533" and
// MaxMeasuredValue "min minMeasuredValue + 1"; 0 / 65534 is the widest
// pair both constraints allow.
const (
	flowMinMeasuredValue uint16 = 0
	flowMaxMeasuredValue uint16 = 65534
)

// FlowServer projects a [contract.FloatMeasurementSource] onto Matter
// FlowMeasurement (0x0404), the one mandatory application cluster of the
// FlowSensor device type (0x0306) and an optional one on Pump (0x0303).
//
// Model unit: m³/h. Wire unit: uint16 with `MeasuredValue = 10 x Flow`
// (matter.js packages/model/src/standard/resources/flow-measurement.resource.ts:19-24),
// so one wire unit is 0.1 m³/h. The value is rounded and clamped to
// [0, 65534]: a flow cannot be negative on this cluster, and 65535 is the
// null sentinel a real reading must never collide with.
//
// matter.js FlowMeasurementServer
// (packages/node/src/behaviors/flow-measurement/FlowMeasurementServer.ts)
// adds nothing to the generated behavior: every attribute is host state.
// This server is the same shape — the reading comes from the source on
// every read, nothing is cached here.
//
// The revision is read from the generated schema rather than restated,
// so the next snapshot regeneration moves it without a second edit.
type FlowServer struct {
	cluster.DataVersionTracker
	src contract.FloatMeasurementSource
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*FlowServer)(nil)
	_ contract.ClusterDataVersion     = (*FlowServer)(nil)
	_ contract.ClusterAttributeLister = (*FlowServer)(nil)
)

// NewFlowServer constructs a FlowServer backed by src.
func NewFlowServer(src contract.FloatMeasurementSource) *FlowServer {
	return &FlowServer{src: src}
}

// FlowRevision returns the FlowMeasurement cluster revision from the
// generated matter.js schema snapshot.
func FlowRevision() uint16 { return schema.ClusterRevisions[ClusterFlowMeasurement] }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *FlowServer) MatterDataVersion() uint32 { return s.Current() }

// MatterClusterID returns the FlowMeasurement cluster ID (0x0404).
func (*FlowServer) MatterClusterID() uint32 { return ClusterFlowMeasurement }

// MatterRead resolves an attribute by ID against the underlying source.
func (s *FlowServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case attrMeasuredValue:
		// No reading yet — TLV null, the spec's "unknown"
		// (flow-measurement.resource.ts:23); see TemperatureServer.MatterRead.
		if s.src == nil {
			return nil, true
		}
		v, ok := s.src.MatterFloatValue()
		if !ok {
			return nil, true
		}
		return flowToMatter(v), true
	case attrMinMeasuredValue:
		return flowMinMeasuredValue, true
	case attrMaxMeasuredValue:
		return flowMaxMeasuredValue, true
	case attrTolerance:
		// Optional, default 0 (flow-measurement.element.ts:28-31) —
		// published like the neighbouring measurement servers do.
		return uint16(0), true
	case cluster.AttrGlobalFeatureMap:
		// FlowMeasurement defines no features.
		return uint32(0), true
	case cluster.AttrGlobalClusterRevision:
		return FlowRevision(), true
	}
	return nil, false
}

// MatterWrite returns errReadOnly — every FlowMeasurement attribute is
// access "R V".
func (*FlowServer) MatterWrite(context.Context, uint32, any) error {
	return errReadOnly
}

// MatterInvoke returns errNoCommands — FlowMeasurement has no commands.
func (*FlowServer) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, fmt.Errorf("%w (cmd 0x%02X)", errNoCommands, cmdID)
}

// MatterReportable returns the attribute that moves with the reading.
func (*FlowServer) MatterReportable() []uint32 { return []uint32{attrMeasuredValue} }

// MatterAttributes lists every FlowMeasurement attribute the server
// answers, in id order and without the globals.
func (*FlowServer) MatterAttributes() []uint32 {
	return []uint32{attrMeasuredValue, attrMinMeasuredValue, attrMaxMeasuredValue, attrTolerance}
}

// flowToMatter converts m³/h to the wire's 0.1 m³/h unit, rounded and
// clamped to the representable non-null range.
func flowToMatter(m3h float64) uint16 {
	v := math.Round(m3h * 10)
	switch {
	case math.IsNaN(v), v < float64(flowMinMeasuredValue):
		return flowMinMeasuredValue
	case v > float64(flowMaxMeasuredValue):
		return flowMaxMeasuredValue
	}
	return uint16(v)
}
