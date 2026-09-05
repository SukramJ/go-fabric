// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// The fake [endpoint.Store] itself lives in
// [github.com/SukramJ/go-fabric/endpoint/endpointtest], where an external
// consumer can reach it too. These names stay in `package bridge` so both
// the white-box tests in this directory and the black-box tests in `package
// bridge_test` keep using them unqualified, against one implementation.

import (
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// FakeStore is [endpointtest.FakeStore].
type FakeStore = endpointtest.FakeStore

// NewFakeStore returns a fresh in-memory endpoint.Store fake.
func NewFakeStore() *FakeStore {
	return endpointtest.NewFakeStore()
}
