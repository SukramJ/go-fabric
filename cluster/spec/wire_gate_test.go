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

// answered holds resp, the server's answer to cmd, to srv's definition as
// the dispatcher does after the server ran ([spec.CheckResponse]).
func answered(ctx context.Context, srv *spec.Server, cmd uint32, fields, resp any, err error) (any, error) {
	if err != nil {
		return resp, err
	}
	if err := spec.CheckResponse(ctx, srv.Definition(), srv.FeatureMap(), cmd, fields, resp, srv.MatterRead); err != nil {
		return nil, err
	}
	return resp, nil
}

// wireInvoke invokes cmd on srv as a command over the wire reaches it: the
// request gate ([admit]), the server, the response check ([answered]). A
// direct MatterInvoke is a local call and is validated by neither.
func wireInvoke(ctx context.Context, srv *spec.Server, cmd uint32, fields any) (any, error) {
	if err := admit(ctx, srv, cmd, fields); err != nil {
		return nil, err
	}
	resp, err := srv.MatterInvoke(ctx, cmd, fields)
	return answered(ctx, srv, cmd, fields, resp, err)
}

// wireInvokeHeld is [wireInvoke] for a self-held server: the gate,
// [spec.InvokeHeld], the response check.
func wireInvokeHeld(ctx context.Context, srv *spec.Server, held *spec.SelfHeld, cmd uint32, fields any) (any, error) {
	if err := admit(ctx, srv, cmd, fields); err != nil {
		return nil, err
	}
	resp, err := spec.InvokeHeld(ctx, held, cmd, fields)
	return answered(ctx, srv, cmd, fields, resp, err)
}

// decodesAs is a test definition's decoder whose generated form is T: the
// definition's checks judge a payload of type T (spec.payloadType), as
// they judge a generated definition's own request and response types.
func decodesAs[T any]() func(spec.Node) (any, error) {
	return func(spec.Node) (any, error) { var v T; return v, nil }
}
