// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
)

// wireInvoke invokes cmd on srv as a command over the wire reaches it: the
// request held to the definition first, with the server's FeatureMap and
// attributes ([spec.AdmitRequest], as endpoint.TopologyDispatcher holds
// every wire command before MatterInvoke), then the server, then its
// response ([spec.CheckResponse]). A direct MatterInvoke is a local call
// and is validated by neither.
func wireInvoke(ctx context.Context, srv contract.ClusterServer, cmd uint32, fields any) (any, error) {
	var features uint32
	if v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap); ok {
		features, _ = v.(uint32)
	}
	def := spec.Lookup(srv.MatterClusterID())
	if err := spec.AdmitRequest(ctx, def, features, cmd, fields, srv.MatterRead); err != nil {
		return nil, err
	}
	resp, err := srv.MatterInvoke(ctx, cmd, fields)
	if err != nil {
		return resp, err
	}
	if err := spec.CheckResponse(ctx, def, features, cmd, fields, resp, srv.MatterRead); err != nil {
		return nil, err
	}
	return resp, nil
}
