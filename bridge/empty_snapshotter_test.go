// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// The empty-fleet snapshotter itself lives in
// [github.com/SukramJ/go-fabric/endpoint/endpointtest]; this name stays in
// `package bridge` so the tests in this directory and in `package
// bridge_test` keep reaching it unqualified, and so the [Snapshotter]
// conversion is written once.

import (
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// NewEmptySnapshotter returns a [Snapshotter] over an empty fleet — root
// plus aggregator, no bridged endpoints.
func NewEmptySnapshotter() Snapshotter {
	return endpointtest.NewEmptySnapshotter()
}
