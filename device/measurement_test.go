// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// reading is a host's live temperature reading.
type reading struct {
	class contract.MeasurementClass
	value float64
}

func (r reading) MatterMeasurementClass() contract.MeasurementClass { return r.class }
func (r reading) MatterFloatValue() (float64, bool)                 { return r.value, true }

// clusterIDs are the cluster ids of servers, in order.
func clusterIDs(servers []contract.ClusterServer) []uint32 {
	out := make([]uint32, 0, len(servers))
	for _, srv := range servers {
		out = append(out, srv.MatterClusterID())
	}
	return out
}

// TestWithMeasurement: the measurement cluster a TemperatureSensor
// mandates is served from the host's reading by the assembler
// (endpoint.Spec.Measurement), not by a default server of the device; the
// endpoint conforms to its device type and reads the live value.
func TestWithMeasurement(t *testing.T) {
	t.Parallel()
	src := &reading{class: contract.MeasurementTemperature, value: 21.5}
	d, err := NewTemperatureSensor(testOptions("t"), WithMeasurement(src))
	if err != nil {
		t.Fatal(err)
	}
	if d.TemperatureMeasurement() != nil || d.Server(measurement.ClusterTemperatureMeasurement) != nil {
		t.Error("the device mounts a server for the measured cluster")
	}
	if len(d.Servers()) != 0 {
		t.Errorf("device servers %v, want none", clusterIDs(d.Servers()))
	}
	s := d.Spec()
	if s.Measurement != src || s.Source == nil {
		t.Fatalf("Spec Measurement %v, Source %v", s.Measurement, s.Source)
	}
	topo := assemble(t, d.Device)
	ep := bridgedEndpoint(t, topo)
	ids := clusterIDs(endpoint.ClusterServers(ep))
	if !slices.Contains(ids, measurement.ClusterTemperatureMeasurement) {
		t.Fatalf("assembled clusters %v lack TemperatureMeasurement", ids)
	}
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == ep.ID {
			t.Errorf("device-type violation: %v", v)
		}
	}
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv.MatterClusterID() != measurement.ClusterTemperatureMeasurement {
			continue
		}
		if v, _ := srv.MatterRead(0x0000); v != int16(2150) {
			t.Errorf("MeasuredValue %v (%T), want 2150", v, v)
		}
	}

	// Without the option the type is unchanged: a default server.
	plain, err := NewTemperatureSensor(testOptions("p"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.TemperatureMeasurement() == nil || plain.Spec().Measurement != nil {
		t.Error("a TemperatureSensor without WithMeasurement changed")
	}
}

// TestWithMeasurementComposesWithTheDevicesOwnServers: a device with
// clusters of its own keeps them ahead of the measured ones
// (endpoint/materialize.go ClusterServers: Source, then Measurement).
func TestWithMeasurementComposesWithTheDevicesOwnServers(t *testing.T) {
	t.Parallel()
	src := &reading{class: contract.MeasurementFlow, value: 3}
	host := &hostServer{id: 0xFFF1_FC00}
	d, err := NewFlowSensor(testOptions("f"), WithMeasurement(src), WithServer(host))
	if err != nil {
		t.Fatal(err)
	}
	topo := assemble(t, d.Device)
	ids := clusterIDs(endpoint.ClusterServers(bridgedEndpoint(t, topo)))
	hostAt, flowAt := slices.Index(ids, host.id), slices.Index(ids, measurement.ClusterFlowMeasurement)
	if hostAt < 0 || flowAt < 0 || hostAt > flowAt {
		t.Errorf("assembled clusters %v: the host server must precede FlowMeasurement", ids)
	}
}

// TestWithMeasurementRefusals: nil, a source of no materialisable shape,
// and an option for a measured cluster.
func TestWithMeasurementRefusals(t *testing.T) {
	t.Parallel()
	src := &reading{class: contract.MeasurementTemperature, value: 1}
	for name, with := range map[string][]Option{
		"nil":                {WithMeasurement(nil)},
		"no cluster":         {WithMeasurement(&reading{class: contract.MeasurementNone})},
		"server for it":      {WithMeasurement(src), WithServer(&hostServer{id: measurement.ClusterTemperatureMeasurement})},
		"initial for it":     {WithMeasurement(src), WithInitial(measurement.ClusterTemperatureMeasurement, map[uint32]any{0: int16(1)})},
		"opting it in again": {WithMeasurement(src), WithOptional(measurement.ClusterTemperatureMeasurement)},
	} {
		if _, err := NewTemperatureSensor(testOptions("r"), with...); !errors.Is(err, ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", name, err)
		}
	}
}

// bridgedEndpoint is the first bridged endpoint of topo.
func bridgedEndpoint(t *testing.T, topo *endpoint.Topology) *endpoint.Endpoint {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if !ep.IsRoot() && !ep.IsAggregator() {
			return ep
		}
	}
	t.Fatal("no bridged endpoint")
	return nil
}
