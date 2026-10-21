// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"

	tmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// clusterIDs are the cluster ids of servers, in order.
func clusterIDs(servers []contract.ClusterServer) []uint32 {
	out := make([]uint32, 0, len(servers))
	for _, srv := range servers {
		out = append(out, srv.MatterClusterID())
	}
	return out
}

// TestAMeasurementIsDeclaredAndSet: a sensor's measurement cluster is
// declared like any other cluster, and the host's reading reaches it
// through the typed Set — matter.js's host setting the measurement
// behavior's state, which its Datasource reports
// (packages/node/src/behavior/state/managed/Datasource.ts:261-264). The
// endpoint conforms to its device type and reads the value the host set,
// with the host's origin.
func TestAMeasurementIsDeclaredAndSet(t *testing.T) {
	t.Parallel()
	lo, hi := int16(-4000), int16(12500)
	d, err := NewTemperatureSensor(testOptions("t"), tmdef.Declare{
		Initial: tmdef.Patch{MinMeasuredValue: &lo, MaxMeasuredValue: &hi},
	})
	if err != nil {
		t.Fatal(err)
	}
	temp := d.TemperatureMeasurement()
	var origin contract.Origin
	temp.OnChanged(func(_ tmdef.State, c contract.Change) { origin = c.Origin })
	reading := int16(2150)
	if err := temp.Set(tmdef.Patch{MeasuredValue: &reading}); err != nil {
		t.Fatal(err)
	}
	if origin != contract.OriginHostSet {
		t.Errorf("the reading reported %v, want OriginHostSet", origin)
	}
	topo := assemble(t, d.Device)
	ep := bridgedEndpoint(t, topo)
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == ep.ID {
			t.Errorf("device-type violation: %v", v)
		}
	}
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv.MatterClusterID() != tmdef.ClusterID {
			continue
		}
		if v, _ := srv.MatterRead(tmdef.AttrMeasuredValue); v != int16(2150) {
			t.Errorf("MeasuredValue %v (%T), want 2150", v, v)
		}
		if v, _ := srv.MatterRead(tmdef.AttrMinMeasuredValue); v != lo {
			t.Errorf("MinMeasuredValue %v, want %d", v, lo)
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
