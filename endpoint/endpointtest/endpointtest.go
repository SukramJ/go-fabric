// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package endpointtest provides the endpoint-level scaffolding a test needs
// to stand a bridge up: an in-memory [endpoint.Store] and a snapshotter over
// an empty fleet.
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

	"github.com/SukramJ/go-fabric/endpoint"
)

// FakeStore is an in-memory implementation of [endpoint.Store]. It is
// concurrency-naive — callers must not race it — and carries only the state
// the bridge's startup and reassembly paths read back.
type FakeStore struct {
	rows   map[endpoint.SourceKey]endpoint.Record
	nextID uint16
}

// NewFakeStore returns a fresh [FakeStore] with an empty row map and the
// next-id counter initialised at 2: endpoint 0 is the root and endpoint 1
// the aggregator, so bridged endpoints start at 2.
func NewFakeStore() *FakeStore {
	return &FakeStore{
		rows:   make(map[endpoint.SourceKey]endpoint.Record),
		nextID: 2,
	}
}

// GetEndpoint implements [endpoint.Store].
func (s *FakeStore) GetEndpoint(_ context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	rec, ok := s.rows[key]
	if !ok {
		return endpoint.Record{}, endpoint.ErrNotFound
	}
	return rec, nil
}

// UpsertEndpointAssigning implements [endpoint.Store].
func (s *FakeStore) UpsertEndpointAssigning(_ context.Context, rec endpoint.Record) (uint16, error) {
	if rec.EndpointID == 0 {
		rec.EndpointID = s.nextID
		s.nextID++
	}
	s.rows[rec.Key] = rec
	return rec.EndpointID, nil
}

// ListEndpoints implements [endpoint.Store]. An empty scope matches every
// row.
func (s *FakeStore) ListEndpoints(_ context.Context, scope string) ([]endpoint.Record, error) {
	var out []endpoint.Record
	for _, rec := range s.rows {
		if scope == "" || rec.Scope == scope {
			out = append(out, rec)
		}
	}
	return out, nil
}

// RemoveEndpoint implements [endpoint.Store].
func (s *FakeStore) RemoveEndpoint(_ context.Context, key endpoint.SourceKey) error {
	delete(s.rows, key)
	return nil
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
		NodeLabel: assemblerNodeLabel,
	}
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
