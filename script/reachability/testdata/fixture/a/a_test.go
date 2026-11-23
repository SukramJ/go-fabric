// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package a_test

import (
	"testing"

	"thingfabric.dev/go-fabric/a"
)

func TestA(t *testing.T) {
	a.Called()
	var x a.T
	x.Called()
	use(nil)
	a.Annotated()
	useOuter(a.Outer{})
}

func use(a.I) {}

func useOuter(a.Outer) {}
