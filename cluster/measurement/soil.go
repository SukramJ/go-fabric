// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	"github.com/SukramJ/go-fabric/contract"
)

// --- SoilMeasurement (0x0430) ------------------------------------------

// ClusterSoilMeasurement is the SoilMeasurement cluster id, its generated
// definition's.
const ClusterSoilMeasurement = soildef.ClusterID

// soilInst serves the definition's two mandatory attributes; the cluster
// has no features, no optional attributes, commands or events.
var soilInst = mustInstance(soildef.Definition, spec.Options{})

// SoilMoistureSource is the read surface of a soil moisture sensor: the
// reading in percent ([contract.FloatMeasurementSource], class
// [contract.MeasurementSoilMoisture]) and the measurement limits, which
// the host fixes. The limits are the SoilMoistureMeasurementLimits
// attribute (quality F) as the host states it; connectedhomeip takes them
// at construction and never changes them
// (src/app/clusters/soil-measurement-server/SoilMeasurementCluster.cpp
// :25-31 at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20).
type SoilMoistureSource interface {
	contract.FloatMeasurementSource
	MatterSoilMoistureLimits() soildef.MeasurementAccuracyStruct
}

// SoilMeasurementServer projects a [SoilMoistureSource] onto Matter
// SoilMeasurement (0x0430), the one mandatory application cluster of the
// SoilSensor device type (0x0045).
//
// matter.js has no server logic for it:
// packages/node/src/behaviors/soil-measurement/SoilMeasurementServer.ts
// :14 is `export class SoilMeasurementServer extends
// SoilMeasurementBehavior {}`. The rules are connectedhomeip's
// SoilMeasurementCluster.cpp at the harness pin:
//
//   - SoilMoistureMeasurementLimits is the host's struct, fixed at
//     construction and read back as given (:25-28, :38-39).
//   - SoilMoistureMeasuredValue starts null (:30) and stays null until the
//     source has a reading.
//   - A reading outside [MinMeasuredValue, MaxMeasuredValue] is refused
//     with CONSTRAINT_ERROR and the attribute keeps its previous value
//     (SetSoilMoistureMeasuredValue :58-71). The reading here is pulled
//     from the source on every read, so a refused reading is never
//     stored: the read answers the last reading that was inside the
//     limits, or null when there was none.
//
// Model unit: percent. The reading is rounded to the attribute's whole
// percent (type "percent", uint8).
type SoilMeasurementServer struct {
	cluster.DataVersionTracker
	src    SoilMoistureSource
	limits soildef.MeasurementAccuracyStruct

	// mu guards last and the read tickets; it is not held while the
	// source is read (measured).
	mu   sync.Mutex
	last *uint8 // the last reading inside the limits; nil until one arrives
	// taken and applied order the reads: a reading is applied only when
	// no later one was, so two reads that overlap leave the newer.
	taken, applied uint64
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*SoilMeasurementServer)(nil)
	_ contract.ClusterDataVersion     = (*SoilMeasurementServer)(nil)
	_ contract.ClusterAttributeLister = (*SoilMeasurementServer)(nil)
)

// NewSoilMeasurementServer constructs a SoilMeasurementServer backed by
// src, with the limits src states.
func NewSoilMeasurementServer(src SoilMoistureSource) *SoilMeasurementServer {
	return &SoilMeasurementServer{src: src, limits: src.MatterSoilMoistureLimits()}
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *SoilMeasurementServer) MatterDataVersion() uint32 { return s.Current() }

// MatterClusterID returns the SoilMeasurement cluster ID (0x0430).
func (*SoilMeasurementServer) MatterClusterID() uint32 { return ClusterSoilMeasurement }

// MatterRead resolves an attribute by ID (SoilMeasurementCluster.cpp
// ReadAttribute :33-49).
func (s *SoilMeasurementServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case soildef.AttrSoilMoistureMeasurementLimits:
		return s.limits, true
	case soildef.AttrSoilMoistureMeasuredValue:
		if v := s.measured(); v != nil {
			return *v, true
		}
		return nil, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return soilInst.ReadGlobal(attrID)
	}
	return nil, false
}

// measured is the value SoilMoistureMeasuredValue reads: the source's
// reading when it lies inside the limits, else the last one that did; nil
// for null.
func (s *SoilMeasurementServer) measured() *uint8 {
	// The source is host code: it is read with mu released (ADR 0026 P6).
	s.mu.Lock()
	s.taken++
	ticket := s.taken
	s.mu.Unlock()
	v, ok := s.src.MatterFloatValue()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ticket < s.applied {
		return s.last // a later read was applied first
	}
	s.applied = ticket
	if !ok {
		// No reading — null, as the attribute starts (:30) and as a
		// null set stores it (:61, :68).
		s.last = nil
		return nil
	}
	if pct, inside := s.withinLimits(v); inside {
		s.last = &pct
	}
	return s.last
}

// withinLimits rounds v to a whole percent and reports whether it lies in
// [MinMeasuredValue, MaxMeasuredValue] (SetSoilMoistureMeasuredValue
// :63-65); NaN and values outside the uint8 range never do.
func (s *SoilMeasurementServer) withinLimits(v float64) (uint8, bool) {
	r := math.Round(v)
	if math.IsNaN(r) || r < 0 || r > math.MaxUint8 {
		return 0, false
	}
	pct := uint8(r)
	return pct, int64(pct) >= s.limits.MinMeasuredValue && int64(pct) <= s.limits.MaxMeasuredValue
}

// MatterWrite refuses the write as the definition answers it (see
// refuseWrite) — both attributes are access "R V".
func (*SoilMeasurementServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	return refuseWrite(soilInst, attrID, value)
}

// MatterInvoke returns errNoCommands — SoilMeasurement has no commands.
func (*SoilMeasurementServer) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, fmt.Errorf("%w (cmd 0x%02X)", errNoCommands, cmdID)
}

// MatterReportable returns the attribute that moves with the reading.
func (*SoilMeasurementServer) MatterReportable() []uint32 {
	return []uint32{soildef.AttrSoilMoistureMeasuredValue}
}

// MatterAttributes lists both SoilMeasurement attributes, in id order and
// without the globals (SoilMeasurementCluster.cpp Attributes :51-56).
func (*SoilMeasurementServer) MatterAttributes() []uint32 { return soilInst.MatterAttributes() }

// soilServer is the materialiser of [contract.MeasurementSoilMoisture]: a
// src without the limits is not a soil sensor this server can describe,
// and mounts nothing.
func soilServer(src any, _ contract.MeasurementContext) []contract.ClusterServer {
	s, ok := src.(SoilMoistureSource)
	if !ok {
		return nil
	}
	return []contract.ClusterServer{NewSoilMeasurementServer(s)}
}
