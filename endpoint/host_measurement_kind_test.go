// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// This file is the milestone check for the open measurement-kind set: it
// stands in for a host that wants to bridge a reading the library has
// never heard of. It is an external test package and imports nothing but
// the exported API — contract, endpoint and endpoint/endpointtest — so
// every step it takes is a step a host outside this module can take. In
// particular it does NOT import cluster/measurement: the built-in
// materialisers reach the registry through endpoint's own dependency on
// that package, exactly as they would in a host binary.
package endpoint_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/eligibility"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// The host's own Matter projection: Flow Sensor (device type 0x0306)
// carrying FlowMeasurement (cluster 0x0404). Neither is modelled by this
// library, which is the point — the kind is registered from out here,
// with the cluster server the host wrote, and nothing in contract,
// cluster/measurement or endpoint knows it exists.
const (
	hostFlowDeviceType uint16 = 0x0306
	hostFlowClusterID  uint32 = 0x0404
	// MeasuredValue (0x0000) per the FlowMeasurement cluster.
	hostFlowMeasuredValue uint32 = 0x0000
)

// hostFlowClass is minted at package init, the way a host mints one in
// its wiring before the bridge serves its first read.
var hostFlowClass = contract.RegisterMeasurementKind(contract.MeasurementKind{
	Name:       "Flow",
	DeviceType: hostFlowDeviceType,
	ClusterID:  hostFlowClusterID,
	Materialize: func(src any, mc contract.MeasurementContext) []contract.ClusterServer {
		f, ok := src.(contract.FloatMeasurementSource)
		if !ok {
			return nil
		}
		// The endpoint id is taken at CONSTRUCTION, which is the shape
		// the library's own GenericSwitch needs and the shape a host
		// could not express while the materialiser saw only the source.
		return []contract.ClusterServer{&hostFlowServer{src: f, endpoint: mc.EndpointID}}
	},
})

// hostFlowSource is the host's model object: a float reading that
// declares itself as the host-registered class.
type hostFlowSource struct {
	value float64
}

func (s *hostFlowSource) MatterMeasurementClass() contract.MeasurementClass { return hostFlowClass }

func (s *hostFlowSource) MatterFloatValue() (float64, bool) { return s.value, true }

// hostFlowServer is the host's cluster server, written against
// [contract.ClusterServer] alone. It keeps the endpoint id its
// materialiser was handed, standing in for every host cluster that has
// to address a Matter path of its own — an event source, or a cluster
// whose attributes name the endpoint they belong to.
type hostFlowServer struct {
	src      contract.FloatMeasurementSource
	endpoint uint16
}

func (s *hostFlowServer) MatterClusterID() uint32 { return hostFlowClusterID }

func (s *hostFlowServer) MatterRead(attrID uint32) (any, bool) {
	if attrID != hostFlowMeasuredValue {
		return nil, false
	}
	v, observed := s.src.MatterFloatValue()
	if !observed {
		return nil, false
	}
	// Wire scale for FlowMeasurement is tenths of m³/h.
	return uint16(v * 10), true //nolint:gosec // test fixture value range is controlled
}

func (s *hostFlowServer) MatterWrite(context.Context, uint32, any) error { return nil }

func (s *hostFlowServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil //nolint:nilnil // command-less cluster
}

func (s *hostFlowServer) MatterReportable() []uint32 { return []uint32{hostFlowMeasuredValue} }

// TestHostRegisteredMeasurementKindMaterialisesAnEndpoint is the
// milestone: a measurement kind registered from outside the library
// travels the same route a built-in does — the eligibility classifier
// calls the source exposable, and the assembler then actually mounts the
// kind's cluster on a bridged endpoint.
//
// The two halves are asserted together on purpose. The classifier's
// verdict is operator-facing ("this source can be bridged"), so a green
// verdict with no endpoint behind it is the defect this registry exists
// to make unrepresentable; checking only one half would not see it.
func TestHostRegisteredMeasurementKindMaterialisesAnEndpoint(t *testing.T) {
	t.Parallel()

	src := &hostFlowSource{value: 4.2}

	// Half one: the advertised verdict.
	verdict := eligibility.Classify(src)
	if verdict.State != eligibility.StateMappable {
		t.Fatalf("Classify(host source) state = %v (%s), want Mappable", verdict.State, verdict.Reason)
	}
	if verdict.DeviceType != hostFlowDeviceType {
		t.Errorf("Classify(host source) device type = 0x%04X, want 0x%04X", verdict.DeviceType, hostFlowDeviceType)
	}
	if len(verdict.Clusters) != 1 || verdict.Clusters[0] != hostFlowClusterID {
		t.Errorf("Classify(host source) clusters = %v, want [0x%04X]", verdict.Clusters, hostFlowClusterID)
	}

	// Half two: the endpoint the verdict promises.
	assembler, err := endpoint.New(endpointtest.NewFakeStore(), endpoint.Config{
		VendorID:  0xFFF1,
		ProductID: 0x8001,
		NodeLabel: "Host Bridge",
	}, nil)
	if err != nil {
		t.Fatalf("endpoint.New: %v", err)
	}
	topology, err := assembler.Assemble(context.Background(), []endpoint.Snapshot{{
		Scope:         "host",
		ModelComplete: true,
		Endpoints: []endpoint.Spec{{
			StableKey:    endpoint.StringKey("host:flow-1"),
			DeviceType:   contract.MeasurementClassDeviceType(hostFlowClass),
			FriendlyName: "Garden Flow",
			Measurement:  src,
		}},
	}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	bridged := topology.Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d bridged endpoints, want 1", len(bridged))
	}
	ep := bridged[0]
	if ep.DeviceType != hostFlowDeviceType {
		t.Errorf("bridged endpoint device type = 0x%04X, want 0x%04X", ep.DeviceType, hostFlowDeviceType)
	}

	var flow contract.ClusterServer
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv != nil && srv.MatterClusterID() == hostFlowClusterID {
			flow = srv
			break
		}
	}
	if flow == nil {
		t.Fatalf("no cluster 0x%04X on the bridged endpoint; the registry advertised an exposure the assembler did not build", hostFlowClusterID)
	}
	got, ok := flow.MatterRead(hostFlowMeasuredValue)
	if !ok || got != uint16(42) {
		t.Errorf("MeasuredValue = %v, %v; want 42, true — the mounted server is not reading the host source", got, ok)
	}
}

// TestHostRegisteredMeasurementKindReceivesTheEndpointID is the second
// milestone: the kind is registered from outside the library, and the
// server the assembler mounts for it knows which endpoint it lives on.
//
// The id has to arrive through [contract.MeasurementContext] at
// materialise time, not from a later stamp: [endpoint.ClusterServers]
// rebuilds the whole set on every dispatch, so anything written onto a
// server after that call is discarded with it. That is why the two
// library shapes that need the id — GenericSwitch's event address and
// PowerSource's EndpointList — were hard-coded in the assembler until
// the signature could carry it.
//
// Asserting equality with ep.ID rather than a literal keeps the test
// honest about what it measures; the separate zero check is what makes
// the equality mean something, since a materialiser that ignored the
// context entirely would also read 0 on both sides.
func TestHostRegisteredMeasurementKindReceivesTheEndpointID(t *testing.T) {
	t.Parallel()

	assembler, err := endpoint.New(endpointtest.NewFakeStore(), endpoint.Config{
		VendorID:  0xFFF1,
		ProductID: 0x8001,
		NodeLabel: "Host Bridge",
	}, nil)
	if err != nil {
		t.Fatalf("endpoint.New: %v", err)
	}
	topology, err := assembler.Assemble(context.Background(), []endpoint.Snapshot{{
		Scope:         "host",
		ModelComplete: true,
		Endpoints: []endpoint.Spec{{
			StableKey:    endpoint.StringKey("host:flow-endpointed"),
			DeviceType:   contract.MeasurementClassDeviceType(hostFlowClass),
			FriendlyName: "Addressed Flow",
			Measurement:  &hostFlowSource{value: 1.0},
		}},
	}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	bridged := topology.Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d bridged endpoints, want 1", len(bridged))
	}
	ep := bridged[0]
	if ep.ID == 0 {
		t.Fatal("bridged endpoint id is 0; the equality below would hold for a materialiser that ignored the context")
	}

	var flow *hostFlowServer
	for _, srv := range endpoint.ClusterServers(ep) {
		if got, ok := srv.(*hostFlowServer); ok {
			flow = got
			break
		}
	}
	if flow == nil {
		t.Fatalf("no cluster 0x%04X on the bridged endpoint", hostFlowClusterID)
	}
	if flow.endpoint != ep.ID {
		t.Errorf("host server endpoint = %d, want %d — the materialiser was not given the endpoint it is mounted on", flow.endpoint, ep.ID)
	}
}
