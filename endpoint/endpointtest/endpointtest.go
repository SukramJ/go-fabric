// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package endpointtest provides the endpoint-level scaffolding a test needs
// to stand a bridge up: an in-memory [endpoint.Store] and a snapshotter over
// an empty fleet — [AssertDeviceTypeConformance], which holds a host's
// assembled topology to its device types, and [AssertClusterServerContract],
// which holds a host's cluster server to the read contract.
//
// It is the test-support counterpart of package endpoint, in the shape
// net/http/httptest has to net/http, and it is separate from
// bridge/bridgetest for a structural reason rather than a taxonomic one:
// nothing here refers to the bridge, so the bridge's own white-box tests can
// import it. A home under bridge/ would import bridge, and an in-package
// bridge test importing that would close a cycle — which is what left these
// two fakes stranded in _test.go files, invisible to every consumer, in the
// first place.
package endpointtest

import (
	"context"
	"slices"
	"strconv"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/store"
)

// AssertDeviceTypeConformance fails tb for every device type violation
// [endpoint.ValidateDeviceTypes] finds in topology — a host's test that its
// projection produces endpoints its device types allow, with every
// mandatory cluster, feature, attribute, command and event in place.
// Publish the root's and the Aggregator's cluster servers on topology
// first ([endpoint.Endpoint.PublishClusterServers]); without them those two
// endpoints are judged empty.
//
// tolerate lists the violations the host accepts knowingly, each as
// "<endpoint>/<kind> <requirement>" ("9/instanceCount device:PowerSource",
// [endpoint.DeviceTypeViolation.Key] behind the endpoint id). A tolerated
// entry that matches no violation fails tb too, so the list cannot outlive
// the reason for it. It returns the violations found, tolerated ones
// included.
func AssertDeviceTypeConformance(tb testing.TB, topology *endpoint.Topology, tolerate ...string) []endpoint.DeviceTypeViolation {
	tb.Helper()
	found := endpoint.ValidateDeviceTypes(topology)
	matched := make([]bool, len(tolerate))
	for i := range found {
		v := &found[i]
		key := strconv.FormatUint(uint64(v.Endpoint), 10) + "/" + v.Key()
		if at := slices.Index(tolerate, key); at >= 0 {
			matched[at] = true
			continue
		}
		tb.Errorf("device type violation: %s", v)
	}
	for i, ok := range matched {
		if !ok {
			tb.Errorf("tolerated device type violation %q no longer occurs", tolerate[i])
		}
	}
	return found
}

// FakeStore is an in-memory implementation of [endpoint.Store]: the
// module's own in-memory store, [memstore.Store], under the name the tests
// know. It numbers as every implementation of the port must — a number
// stored for a source wins over the one a re-stated record carries, the
// counter never rewinds, wraps to 2 after 65534 and skips the numbers
// still held, and [FakeStore.ListEndpoints] lists ascending by number — so
// a test on the fake holds the bridge to the behaviour the real stores
// have (review II, R2-P6-test-0612). Safe for concurrent use.
type FakeStore struct {
	s *memstore.Store
}

// NewFakeStore returns a fresh, empty [FakeStore]: endpoint 0 is the root
// and endpoint 1 the aggregator, so bridged endpoints start at 2.
func NewFakeStore() *FakeStore {
	return &FakeStore{s: memstore.New()}
}

// GetEndpoint implements [endpoint.Store].
func (s *FakeStore) GetEndpoint(ctx context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	return s.s.GetEndpoint(ctx, key)
}

// UpsertEndpointAssigning implements [endpoint.Store].
func (s *FakeStore) UpsertEndpointAssigning(ctx context.Context, rec endpoint.Record) (uint16, error) {
	return s.s.UpsertEndpointAssigning(ctx, rec)
}

// ListEndpoints implements [endpoint.Store]. An empty scope matches every
// row.
func (s *FakeStore) ListEndpoints(ctx context.Context, scope string) ([]endpoint.Record, error) {
	return s.s.ListEndpoints(ctx, scope)
}

// RemoveEndpoint implements [endpoint.Store].
func (s *FakeStore) RemoveEndpoint(ctx context.Context, key endpoint.SourceKey) error {
	return s.s.RemoveEndpoint(ctx, key)
}

// EraseEndpoints implements [endpoint.Store]: every row goes and the
// next-id counter restarts at 2.
func (s *FakeStore) EraseEndpoints(ctx context.Context) error {
	return s.s.EraseEndpoints(ctx)
}

// Identity the test assembler stamps on the root endpoint. Any non-zero
// vendor / product pair and a non-empty label satisfy the assembler's
// validation; the values carry no other meaning.
const (
	assemblerVendorID  uint16 = 0x1234
	assemblerProductID uint16 = 0x5678
	assemblerNodeLabel        = "test-bridge"
)

// AssemblerConfig returns the [endpoint.Config] [NewEmptySnapshotter]
// assembles under.
//
// It is exported for the test that needs a POPULATED fleet, which this
// package cannot build for it — the device specs are the host's — and which
// therefore has to construct its own assembler. Sharing the config keeps
// that assembler's node identity the same as the empty one's, so a reader
// comparing two topologies sees one identity rather than two, and a
// consumer's fixtures do not have to guess values that satisfy the
// assembler's validation.
func AssemblerConfig() endpoint.Config {
	return endpoint.Config{
		VendorID:  assemblerVendorID,
		ProductID: assemblerProductID,
		Fabrics:   EveryFabric,
	}
}

// EveryFabric is a fabric source that lists every fabric index, 1 to 254:
// a test's Binding servers take a request's write from any fabric, as a
// node's would from the fabrics it holds ([endpoint.Config.Fabrics];
// review II, R2-P6-store-1010, decision 158).
var EveryFabric mattercore.FabricSource = everyFabric{}

type everyFabric struct{}

func (everyFabric) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	out := make([]store.FabricRecord, 0, 254)
	for f := 1; f <= 254; f++ {
		out = append(out, store.FabricRecord{FabricIndex: uint8(f)})
	}
	return out, nil
}

// NewEmptySnapshotter returns a snapshot callback over an empty fleet: one
// assembler backed by one in-memory store, assembling no device snapshots.
// The returned func is assignable to bridge.Snapshotter.
//
// It stands in for the composition a host performs at start-up — walk its
// own device model, assemble a topology, hand the bridge the result. The
// bridge neither assembles nor knows the host model, so a test that wants a
// topology has to do what the host does. Only the empty fleet is built here:
// a populated one needs host device types to project from, which is the host
// adapter's business.
//
// The assembler and its store are captured once, so endpoint ids persist
// across repeated calls exactly as they do against a real store.
func NewEmptySnapshotter() func(ctx context.Context) (*endpoint.Topology, error) {
	asm, err := endpoint.New(NewFakeStore(), AssemblerConfig(), nil)
	return func(ctx context.Context) (*endpoint.Topology, error) {
		if err != nil {
			return nil, err
		}
		return asm.Assemble(ctx, nil)
	}
}
