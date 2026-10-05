// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	matterparity "github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// flowSnapshot is the FlowMeasurement entry of parity/schema.json with
// the columns these tests read — the matter.js HEAD pin, not a second
// hand-typed copy of the same numbers.
type flowSnapshot struct {
	ID         uint32 `json:"id"`
	Revision   uint16 `json:"revision"`
	FeatureMap uint32 `json:"featureMap"`
	Attributes []struct {
		ID          uint32 `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Conformance string `json:"conformance"`
		Access      string `json:"access"`
		Constraint  string `json:"constraint"`
		Quality     string `json:"quality"`
	} `json:"attributes"`
	Commands []json.RawMessage `json:"commands"`
	Events   []json.RawMessage `json:"events"`
}

func loadFlowSnapshot(t *testing.T) flowSnapshot {
	t.Helper()
	var s struct {
		Clusters []flowSnapshot `json:"clusters"`
	}
	if err := json.Unmarshal(matterparity.SchemaJSON(), &s); err != nil {
		t.Fatalf("unmarshal schema snapshot: %v", err)
	}
	for _, c := range s.Clusters {
		if c.ID == ClusterFlowMeasurement {
			return c
		}
	}
	t.Fatalf("matter.js schema has no FlowMeasurement (0x%04X)", ClusterFlowMeasurement)
	return flowSnapshot{}
}

// TestParityMatterJS_FlowMeasurementSurface pins the served surface of
// FlowServer against matter.js HEAD flow-measurement.element.ts: the
// revision, the empty FeatureMap, the attribute ids by name, an
// attribute list equal to every non-global attribute matter.js defines
// (all four are M or O, none feature-gated), no commands and no events.
func TestParityMatterJS_FlowMeasurementSurface(t *testing.T) {
	t.Parallel()
	js := loadFlowSnapshot(t)
	srv := NewFlowServer(flowSrc{value: 1, observed: true})

	if FlowRevision() != js.Revision {
		t.Errorf("FlowRevision = %d, want %d", FlowRevision(), js.Revision)
	}
	if rev, _ := schema.ClusterRevision(ClusterFlowMeasurement); rev != js.Revision {
		t.Errorf("schema.ClusterRevision(0x0404) = %d, want %d — regenerate schema/", rev, js.Revision)
	}
	if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != js.FeatureMap {
		t.Errorf("FeatureMap = %v, want %d", fm, js.FeatureMap)
	}

	byName := map[string]uint32{
		"MeasuredValue":    attrMeasuredValue,
		"MinMeasuredValue": attrMinMeasuredValue,
		"MaxMeasuredValue": attrMaxMeasuredValue,
		"Tolerance":        attrTolerance,
	}
	var want []uint32
	for _, a := range js.Attributes {
		if a.ID >= 0xFFF0 {
			continue
		}
		id, ok := byName[a.Name]
		if !ok {
			t.Errorf("matter.js FlowMeasurement attribute %q (0x%04X) has no constant here", a.Name, a.ID)
			continue
		}
		if id != a.ID {
			t.Errorf("%s = 0x%04X, want 0x%04X", a.Name, id, a.ID)
		}
		if a.Type != "uint16" {
			t.Errorf("%s type = %q in matter.js; the server encodes uint16", a.Name, a.Type)
		}
		if a.Access != "R V" {
			t.Errorf("%s access = %q; the server treats every attribute as read-only", a.Name, a.Access)
		}
		want = append(want, a.ID)
	}
	slices.Sort(want)
	if got := srv.MatterAttributes(); !slices.Equal(got, want) {
		t.Errorf("MatterAttributes = %v, want %v (matter.js)", got, want)
	}
	if len(js.Commands) != 0 || len(js.Events) != 0 {
		t.Errorf("matter.js FlowMeasurement has %d commands / %d events; the server serves none", len(js.Commands), len(js.Events))
	}
}

// TestParityMatterJS_FlowMeasurementConstraints holds the static range
// to the matter.js constraints: MinMeasuredValue "max 65533",
// MaxMeasuredValue "min minMeasuredValue + 1", Tolerance "max 2048",
// and MeasuredValue "minMeasuredValue to maxMeasuredValue".
func TestParityMatterJS_FlowMeasurementConstraints(t *testing.T) {
	t.Parallel()
	js := loadFlowSnapshot(t)
	constraint := map[string]string{}
	for _, a := range js.Attributes {
		constraint[a.Name] = a.Constraint
	}
	if constraint["MinMeasuredValue"] != "max 65533" ||
		constraint["MaxMeasuredValue"] != "min minMeasuredValue + 1" ||
		constraint["Tolerance"] != "max 2048" ||
		constraint["MeasuredValue"] != "minMeasuredValue to maxMeasuredValue" {
		t.Fatalf("matter.js FlowMeasurement constraints moved: %v — re-derive the static range", constraint)
	}

	srv := NewFlowServer(flowSrc{value: 1e9, observed: true})
	minV, _ := srv.MatterRead(attrMinMeasuredValue)
	maxV, _ := srv.MatterRead(attrMaxMeasuredValue)
	tol, _ := srv.MatterRead(attrTolerance)
	measured, _ := srv.MatterRead(attrMeasuredValue)
	lo, _ := minV.(uint16)
	hi, _ := maxV.(uint16)
	if lo > 65533 {
		t.Errorf("MinMeasuredValue %d violates max 65533", lo)
	}
	if hi < lo+1 {
		t.Errorf("MaxMeasuredValue %d violates min MinMeasuredValue+1 (%d)", hi, lo+1)
	}
	if hi == 0xFFFF {
		t.Error("MaxMeasuredValue is the nullable uint16 null sentinel")
	}
	if v, _ := tol.(uint16); v > 2048 {
		t.Errorf("Tolerance %d violates max 2048", v)
	}
	if v, _ := measured.(uint16); v < lo || v > hi {
		t.Errorf("a saturated MeasuredValue %d leaves [%d, %d]", v, lo, hi)
	}
}

// TestParityMatterJS_FlowSensorDeviceType pins the measurement-class
// projection to the FlowSensor device type matter.js defines around
// this cluster (flow-sensor.element.ts): device type 0x0306, the
// cluster mandatory there.
func TestParityMatterJS_FlowSensorDeviceType(t *testing.T) {
	t.Parallel()
	dt := contract.MeasurementClassDeviceType(contract.MeasurementFlow)
	if name, _ := schema.DeviceTypeName(uint32(dt)); name != "FlowSensor" {
		t.Errorf("MeasurementFlow device type 0x%04X is %q in matter.js, want FlowSensor", dt, name)
	}
	if contract.MeasurementClassClusterID(contract.MeasurementFlow) != ClusterFlowMeasurement {
		t.Error("MeasurementFlow does not project to FlowMeasurement")
	}
	if !schema.DeviceTypeRequiresServerCluster(uint32(dt), ClusterFlowMeasurement) {
		t.Error("FlowSensor does not mandate FlowMeasurement in the snapshot")
	}
}
