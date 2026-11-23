// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/measurement"
	soildef "thingfabric.dev/go-fabric/cluster/spec/soilmeasurement"
	"thingfabric.dev/go-fabric/contract"
)

// soilSource is a soil moisture sensor whose reading the test moves.
type soilSource struct{ value float64 }

func (*soilSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementSoilMoisture
}
func (s *soilSource) MatterFloatValue() (float64, bool) { return s.value, true }
func (*soilSource) MatterSoilMoistureLimits() soildef.MeasurementAccuracyStruct {
	return soildef.MeasurementAccuracyStruct{
		MeasurementType:  soildef.MeasurementTypeSoilMoisture,
		MinMeasuredValue: 0, MaxMeasuredValue: 90,
		AccuracyRanges: []soildef.MeasurementAccuracyRangeStruct{{RangeMin: 0, RangeMax: 90}},
	}
}

// soilReading reads SoilMoistureMeasuredValue through a fresh
// ClusterServers call, as every dispatch does.
func soilReading(t *testing.T, ep *Endpoint) any {
	t.Helper()
	for _, s := range ClusterServers(ep) {
		if s.MatterClusterID() == measurement.ClusterSoilMeasurement {
			v, _ := s.MatterRead(soildef.AttrSoilMoistureMeasuredValue)
			return v
		}
	}
	t.Fatal("no SoilMeasurement server on the endpoint")
	return nil
}

func endpointByKey(t *testing.T, topo *Topology, key SourceKey) *Endpoint {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == key.String() {
			return ep
		}
	}
	t.Fatalf("no endpoint for key %v", key)
	return nil
}

// TestSoilLastReadingSurvivesAMaterialisation (R2-C2-store-1453, decision
// 526): SoilMeasurement's last reading inside the limits lives in the
// server instance, and the endpoint keeps that instance across every
// ClusterServers call and reassembly for as long as the endpoint and its
// source stay — a reading outside the limits then reads the previous one,
// not null. The kept instance goes with the endpoint, and a new source gets
// a new one.
func TestSoilLastReadingSurvivesAMaterialisation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := c3Assembler(t)
	key := StringKey("soil")
	src := &soilSource{value: 40}
	spec := Spec{StableKey: key, DeviceType: 0x0045, FriendlyName: "Soil", Measurement: src}
	assemble := func(specs ...Spec) *Topology {
		t.Helper()
		topo, err := a.Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: specs}})
		if err != nil {
			t.Fatal(err)
		}
		return topo
	}

	ep := endpointByKey(t, assemble(spec), key)
	if v := soilReading(t, ep); v != uint8(40) {
		t.Fatalf("in limits: %v, want 40", v)
	}
	src.value = 95 // outside [0, 90]
	if v := soilReading(t, ep); v != uint8(40) {
		t.Errorf("out of limits on a second ClusterServers call: %v, want the previous 40", v)
	}
	if v := soilReading(t, endpointByKey(t, assemble(spec), key)); v != uint8(40) {
		t.Errorf("out of limits after a reassembly: %v, want the previous 40", v)
	}

	// The endpoint goes and comes back: its kept server went with it.
	assemble(flowSpec(StringKey("other"), "other"))
	if v := soilReading(t, endpointByKey(t, assemble(spec), key)); v != nil {
		t.Errorf("out of limits after the endpoint was removed and re-added: %v, want null", v)
	}

	// A new source under the same key is a new server.
	src.value = 30
	ep = endpointByKey(t, assemble(spec), key)
	if v := soilReading(t, ep); v != uint8(30) {
		t.Fatalf("in limits: %v, want 30", v)
	}
	replaced := spec
	replaced.Measurement = &soilSource{value: 95}
	if v := soilReading(t, endpointByKey(t, assemble(replaced), key)); v != nil {
		t.Errorf("a replaced source out of limits: %v, want null (the old source's reading is not its own)", v)
	}
}
