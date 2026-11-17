// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package deepcopy holds the one list copy the cluster servers share: a
// list of generated values copied entry by entry through each entry's
// generated Clone (clustergen, review II decision 142), so a list a server
// hands out or takes in shares no list, octet string or pointer with the
// state it serves. It replaces the per-package copies the servers grew
// (core, sensing, energy, camera, jointfabric, servicearea, thermo).
package deepcopy

// Each is a deep copy of in, each entry through its own Clone; nil stays
// nil and an empty list stays empty.
func Each[T interface{ Clone() T }](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	for i := range in {
		out[i] = in[i].Clone()
	}
	return out
}
