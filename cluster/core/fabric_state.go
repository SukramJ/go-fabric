// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "slices"

// fabricsOf returns the distinct fabric indices of entries, ascending: a
// server's [contract.FabricScopedState] MatterHeldFabrics over its list.
func fabricsOf[T any](entries []T, fabric func(T) uint8) []uint8 {
	var out []uint8
	for _, e := range entries {
		if f := fabric(e); !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}
