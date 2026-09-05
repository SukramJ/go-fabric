// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Test-side stand-in for the composition a host performs at start-up: it
// walks its own device model, assembles a topology from it, and hands the
// bridge the result. The bridge itself neither assembles nor knows the
// host model, so tests that want a topology have to do the same thing the
// host does.
//
// Only the empty fleet is built here — root plus aggregator, no bridged
// endpoints. A populated fleet needs host device types to project from,
// which is the host adapter's business and is covered on that side.
//
// Lives in `package bridge` so both the white-box tests in this directory
// and the black-box tests in `package bridge_test` can reach it through
// the exported constructor, the same arrangement [NewFakeStore] uses.

import (
	"context"

	"github.com/SukramJ/go-fabric/endpoint"
)

// Identity the test assembler stamps on the root endpoint. Any non-zero
// vendor / product pair and a non-empty label satisfy the assembler's
// validation; the values match the ones the bridge tests already pass in
// their [Config] so a reader sees one identity, not two.
const (
	testAssemblerVendorID  uint16 = 0x1234
	testAssemblerProductID uint16 = 0x5678
	testAssemblerNodeLabel        = "test-bridge"
)

// NewEmptySnapshotter returns a [Snapshotter] over an empty fleet: one
// assembler backed by one in-memory store, assembling no device
// snapshots. Endpoint ids therefore persist across repeated calls exactly
// as they do against a real store.
func NewEmptySnapshotter() Snapshotter {
	asm, err := endpoint.New(NewFakeStore(), endpoint.Config{
		VendorID:  testAssemblerVendorID,
		ProductID: testAssemblerProductID,
		NodeLabel: testAssemblerNodeLabel,
	}, nil)
	return func(ctx context.Context) (*endpoint.Topology, error) {
		if err != nil {
			return nil, err
		}
		return asm.Assemble(ctx, nil)
	}
}
