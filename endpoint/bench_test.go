// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint_test

// Benchmarks for the two endpoint-side paths a running bridge re-executes
// rather than runs once:
//
//   - Assemble rebuilds the whole topology on every model change the host
//     reports. A host with a live device model reassembles on device
//     add/remove and on a source reload, and the assembly touches the
//     endpoint-id store for every spec each time, so its cost scales with
//     the fleet and not with the change.
//   - ClusterServers materialises an endpoint's cluster surface, and it is
//     re-run per dispatch by design (see the AvailabilityProbe doc comment:
//     a topology-time snapshot cannot express a source that died since).
//     It therefore sits on the read path of every attribute a controller
//     asks for.
//
// Both fixtures are built before the timer starts; the timed loop calls only
// the function under measurement.

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// benchFleetSize is the number of bridged endpoints the assembly benchmarks
// run over. It is large enough that the per-spec work dominates the fixed
// root/aggregator scaffolding, which is what a regression would show up in.
const benchFleetSize = 64

// Sinks. Package-level so the assembly result cannot be discarded.
var (
	benchTopology *endpoint.Topology
	benchServers  []contract.ClusterServer
)

// benchTempSource is a temperature measurement source. The fleet is built
// from measurement sources rather than from a stub [contract.EndpointSource]
// on purpose: a stub source returning no cluster servers makes
// ClusterServers take its "no cluster surface" early return, and the
// benchmark would then measure a branch no bridged endpoint takes in
// production. A measurement source drives the real materialiser plus the
// mandatory Descriptor and BridgedDeviceBasicInformation servers.
type benchTempSource struct{ value float64 }

func (benchTempSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementTemperature
}
func (s benchTempSource) MatterFloatValue() (float64, bool) { return s.value, true }

// benchSpecs builds n bridged-endpoint specs with stable keys, so a repeated
// Assemble over the same store reuses the persisted endpoint ids — which is
// what a reassembly after a model change does.
func benchSpecs(n int) []endpoint.Spec {
	specs := make([]endpoint.Spec, 0, n)
	for i := range n {
		specs = append(specs, endpoint.Spec{
			StableKey:      endpoint.StringKey(fmt.Sprintf("bench|DEV%04d|1|measurement|ACTUAL_TEMPERATURE", i)),
			DeviceAddress:  fmt.Sprintf("DEV%04d", i),
			DeviceType:     contract.MeasurementClassDeviceType(contract.MeasurementTemperature),
			FriendlyName:   fmt.Sprintf("Bench Sensor %d", i),
			ChannelAddress: fmt.Sprintf("DEV%04d:1", i),
			Measurement:    benchTempSource{value: float64(20 + i%5)},
		})
	}
	return specs
}

// benchAssembler returns an assembler over an in-memory store with a
// discarding logger, so neither disk nor log formatting lands in the timed
// region.
func benchAssembler(tb testing.TB) *endpoint.Assembler {
	tb.Helper()
	a, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		tb.Fatalf("endpoint.New: %v", err)
	}
	return a
}

// BenchmarkAssembleReassembly measures a reassembly of an already-known
// fleet: the case a running bridge actually hits, where every spec resolves
// to a persisted endpoint id instead of allocating a new one. One warm-up
// assembly outside the timed region does the id allocation, so the loop
// measures the steady-state path rather than the first-boot one.
func BenchmarkAssembleReassembly(b *testing.B) {
	asm := benchAssembler(b)
	snapshots := []endpoint.Snapshot{{
		Scope:         "bench",
		Endpoints:     benchSpecs(benchFleetSize),
		ModelComplete: true,
	}}
	ctx := context.Background()
	if _, err := asm.Assemble(ctx, snapshots); err != nil {
		b.Fatalf("warm-up Assemble: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		top, err := asm.Assemble(ctx, snapshots)
		if err != nil {
			b.Fatalf("Assemble: %v", err)
		}
		benchTopology = top
	}
}

// BenchmarkClusterServers measures materialising one bridged endpoint's
// cluster surface. The endpoint comes out of a real assembly rather than
// being hand-built, so the benchmark sees the same field population a
// dispatch does — including the per-endpoint state the assembler carries
// across reassemblies.
func BenchmarkClusterServers(b *testing.B) {
	asm := benchAssembler(b)
	top, err := asm.Assemble(context.Background(), []endpoint.Snapshot{{
		Scope:         "bench",
		Endpoints:     benchSpecs(benchFleetSize),
		ModelComplete: true,
	}})
	if err != nil {
		b.Fatalf("Assemble: %v", err)
	}
	bridged := top.Bridged()
	if len(bridged) == 0 {
		b.Fatal("Assemble produced no bridged endpoints")
	}
	ep := bridged[0]
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchServers = endpoint.ClusterServers(ep)
	}
}
