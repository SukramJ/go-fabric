// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// cloneEach deep-copies a list of generated values through their generated
// Clone (clustergen, decision 142): a server hands out and takes in its
// state with it, never with a shallow slices.Clone that shares a slice's
// bytes or a pointer's target. nil stays nil.
func cloneEach[T interface{ Clone() T }](list []T) []T {
	if list == nil {
		return nil
	}
	out := make([]T, len(list))
	for i, v := range list {
		out[i] = v.Clone()
	}
	return out
}
