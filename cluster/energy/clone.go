// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

// cloner is a generated struct with its deep Clone (clustergen, decision
// 142).
type cloner[T any] interface{ Clone() T }

// cloneAll is a deep copy of a list of generated structs, each through its
// generated Clone, nil kept nil: a list a server hands out or takes in
// shares no list, octet string or optional field with its state.
func cloneAll[T cloner[T]](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	for i, v := range in {
		out[i] = v.Clone()
	}
	return out
}
