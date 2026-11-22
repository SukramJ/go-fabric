// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/store"
)

// everyFabricSource lists every fabric index: the package's tests whose
// Binding is not about the fabric check take it (a Binding without a
// source refuses every request write, decision 158).
type everyFabricSource struct{}

func (everyFabricSource) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	out := make([]store.FabricRecord, 0, 254)
	for f := 1; f <= 254; f++ {
		out = append(out, store.FabricRecord{FabricIndex: uint8(f)})
	}
	return out, nil
}

// sourcedBinding is NewBinding with every fabric as its source.
func sourcedBinding() *mattercore.Binding {
	b := mattercore.NewBinding()
	b.SetFabricSource(everyFabricSource{})
	return b
}
