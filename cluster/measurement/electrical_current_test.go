// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	epmdef "thingfabric.dev/go-fabric/cluster/spec/electricalpowermeasurement"
	"thingfabric.dev/go-fabric/contract"
)

// dcReadings is a consolidated electrical group of a direct-current meter.
type dcReadings struct{ specReadings }

func (dcReadings) MatterElectricalCurrent() contract.WiredCurrent { return contract.WiredCurrentDC }

// acReadings states alternating current explicitly.
type acReadings struct{ specReadings }

func (acReadings) MatterElectricalCurrent() contract.WiredCurrent { return contract.WiredCurrentAC }

// TestElectricalPowerCurrentKind holds R2-P6-doc-1465's ElectricalReadings
// half (decision 163): a source that states direct current
// (contract.ElectricalCurrentSource) is served with the DIRC feature and
// PowerMode DC, without the "[ALTC]" Frequency and its Accuracy entry; a
// source that states none, or alternating current, keeps ALTC and PowerMode
// AC, as before.
func TestElectricalPowerCurrentKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		src       ElectricalReadingsSource
		feature   epmdef.Feature
		mode      uint8
		frequency bool
	}{
		{"none stated", specReadings{}, epmdef.FeatureAlternatingCurrent, 2, true},
		{"AC", acReadings{}, epmdef.FeatureAlternatingCurrent, 2, true},
		{"DC", dcReadings{}, epmdef.FeatureDirectCurrent, 1, false},
	} {
		for _, s := range []*ElectricalPowerServer{NewElectricalPowerServerFromReadings(c.src), NewElectricalPowerServer(c.src)} {
			if fm, _ := s.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(c.feature) {
				t.Errorf("%s: FeatureMap %v, want %d", c.name, fm, c.feature)
			}
			if m, _ := s.MatterRead(epmdef.AttrPowerMode); m != c.mode {
				t.Errorf("%s: PowerMode %v, want %d", c.name, m, c.mode)
			}
			if got := slices.Contains(s.MatterAttributes(), epmdef.AttrFrequency); got != c.frequency {
				t.Errorf("%s: Frequency served %v, want %v", c.name, got, c.frequency)
			}
			if _, ok := s.MatterRead(epmdef.AttrFrequency); ok != c.frequency {
				t.Errorf("%s: Frequency read %v, want %v", c.name, ok, c.frequency)
			}
			acc, _ := s.MatterRead(epmdef.AttrAccuracy)
			list, _ := acc.([]AccuracyStruct)
			hasFreq := slices.ContainsFunc(list, func(a AccuracyStruct) bool { return a.MeasurementType == uint16(epmdef.MeasurementTypeFrequency) })
			if hasFreq != c.frequency {
				t.Errorf("%s: Accuracy %+v lists Frequency %v, want %v", c.name, list, hasFreq, c.frequency)
			}
			if n, _ := s.MatterRead(epmdef.AttrNumberOfMeasurementTypes); n != uint8(len(list)) {
				t.Errorf("%s: NumberOfMeasurementTypes %v, want %d", c.name, n, len(list))
			}
		}
	}
}
