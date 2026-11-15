// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// accessingFabricKey is the context key of the request's accessing fabric.
type accessingFabricKey struct{}

// accessingFabricValue is what [WithAccessingFabric] stamps.
type accessingFabricValue struct {
	fabricIndex uint8
	filtered    bool
}

// WithAccessingFabric returns ctx carrying the accessing fabric of the
// Interaction Model request it serves — the FabricIndex of the requesting
// session (0 for a session that has no fabric yet, a PASE session before
// AddNOC) — and the request's FabricFiltered flag. The dispatcher stamps
// it before it hands a request to a cluster server (the im package's
// WithFabricFilter); a host stamps it only to call its own server as a
// request would.
func WithAccessingFabric(ctx context.Context, fabricIndex uint8, filtered bool) context.Context {
	return context.WithValue(ctx, accessingFabricKey{}, accessingFabricValue{fabricIndex: fabricIndex, filtered: filtered})
}

// AccessingFabric reads the accessing fabric and the FabricFiltered flag
// ctx carries ([WithAccessingFabric]). ok is false when no Interaction
// Model request is behind ctx — a local call, which sees every entry
// whole. ok true with fabricIndex 0 is a remote session that has no fabric
// yet: the two must not stand for one another, because the first may see
// what the second may not. A [FabricScopedReader] and a command handler
// read the request's fabric here, without importing the protocol engine.
func AccessingFabric(ctx context.Context) (fabricIndex uint8, filtered, ok bool) {
	v, ok := ctx.Value(accessingFabricKey{}).(accessingFabricValue)
	if !ok {
		return 0, false, false
	}
	return v.fabricIndex, v.filtered, true
}
