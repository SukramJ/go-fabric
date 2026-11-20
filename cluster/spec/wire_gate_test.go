// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// admit holds the request fields of cmd to srv's definition as a wire
// command is held before it reaches the server ([spec.AdmitRequest], run
// by endpoint.TopologyDispatcher with the server's FeatureMap and
// attributes).
func admit(ctx context.Context, srv *spec.Server, cmd uint32, fields any) error {
	return spec.AdmitRequest(ctx, srv.Definition(), srv.FeatureMap(), cmd, fields, srv.MatterRead)
}

// wireInvoke invokes cmd on srv as a command over the wire reaches it: the
// request gate ([admit]), then the server. A direct MatterInvoke is a
// local call and is not validated.
func wireInvoke(ctx context.Context, srv *spec.Server, cmd uint32, fields any) (any, error) {
	if err := admit(ctx, srv, cmd, fields); err != nil {
		return nil, err
	}
	return srv.MatterInvoke(ctx, cmd, fields)
}

// wireInvokeHeld is [wireInvoke] for a self-held server: the gate, then
// [spec.InvokeHeld].
func wireInvokeHeld(ctx context.Context, srv *spec.Server, held *spec.SelfHeld, cmd uint32, fields any) (any, error) {
	if err := admit(ctx, srv, cmd, fields); err != nil {
		return nil, err
	}
	return spec.InvokeHeld(ctx, held, cmd, fields)
}
