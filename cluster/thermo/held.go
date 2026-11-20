// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// heldSource answers the reads of the spec.Server a self-held server
// routes its commands through ([newHeld]) from the server's own state:
// the attributes a request's or a response's conformance names.
type heldSource func(attrID uint32) (any, bool)

func (h heldSource) MatterAttribute(attrID uint32) (any, bool) { return h(attrID) }

// newHeld builds the spec.Server a self-held server that keeps its own
// state routes MatterInvoke through with [spec.InvokeHeld] (review II,
// decision 154): its instance is the server's, its reads are read's, and
// every request command of the definition is handed to invoke — the
// server's own logic, which runs when the host registered no handler. The
// response is checked with the server's attributes resolving the names
// their conformance takes; the request's model check is the endpoint
// dispatcher's ([spec.AdmitRequest], decision 339).
func newHeld(def *spec.Cluster, opts spec.Options, read func(uint32) (any, bool), invoke func(ctx context.Context, cmdID uint32, fields any) (any, error)) (*spec.Server, error) {
	// Every command id of the definition: a response id is never
	// dispatched (the instance accepts requests only), and one that shares
	// a request's id maps to the same handler.
	handlers := map[uint32]spec.CommandHandler{}
	for i := range def.Commands {
		id := def.Commands[i].ID
		handlers[id] = func(ctx context.Context, fields any) (any, error) { return invoke(ctx, id, fields) }
	}
	return spec.NewServer(def, opts, spec.ServerConfig{Source: heldSource(read), Handlers: handlers})
}
