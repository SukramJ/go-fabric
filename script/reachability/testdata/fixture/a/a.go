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
