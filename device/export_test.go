// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

// InitialFor is initialFor for the external tests: the statements of the
// attributes New requires of the named type (initial_values_test.go).
func InitialFor(name string) []Declaration { return initialFor(name) }
