// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package a is the analyzer's fixture: one function reached only from a
// package-level var's initializer, one exported method no test calls.
package a

// V is initialised by ViaInit when a test binary runs the package's init.
var V = ViaInit()

// ViaInit is reached only from V's initializer.
func ViaInit() int { return 1 }

// Called is called by the test.
func Called() {}

// T carries one method the test calls and one it does not.
type T struct{}

// Called is called by the test.
func (T) Called() {}

// NotCalled is called by nothing.
func (*T) NotCalled() {}

// I is an interface; its methods are no code and are not listed.
type I interface{ Method() }

// inner is unexported; its exported method is promoted into Outer.
type inner struct{}

// Promoted is called by nothing; it is listed on Outer, the exported type
// it is promoted into (R2-C2-test-2803).
func (inner) Promoted() {}

// Outer embeds inner.
type Outer struct{ inner }

// Annotated is called by the test and annotated all the same: a redundant
// exemption (R2-C2-test-2802).
// fabric:reachable:reason="the fixture's redundant exemption"
func Annotated() {}
