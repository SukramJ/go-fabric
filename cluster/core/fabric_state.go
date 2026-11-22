// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"

	"thingfabric.dev/go-fabric/store"
)

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

// FabricSource lists the node's fabrics: the fabric-existence source a
// fabric-scoped write is checked against before it is stored, so a write
// of a fabric removed while it was on its way stores nothing under the
// removed index — AccessControl's store, Binding's [BindingConfig.Fabrics]
// and ICD Management's [ICDManagementStore] (review II, R2-P6-store-1010,
// decision 158). store.Port satisfies it.
type FabricSource interface {
	ListFabrics(ctx context.Context) ([]store.FabricRecord, error)
}

// fabricListed reports whether src lists fabric. A listing that fails
// counts as not listed: the caller refuses (fail closed).
func fabricListed(ctx context.Context, src FabricSource, fabric uint8) (bool, error) {
	recs, err := src.ListFabrics(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(recs, func(r store.FabricRecord) bool { return r.FabricIndex == fabric }), nil
}
