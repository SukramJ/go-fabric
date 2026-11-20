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
// every wire command before MatterInvoke), then the server. A direct
// MatterInvoke is a local call and is not validated.
func wireInvoke(ctx context.Context, srv contract.ClusterServer, cmd uint32, fields any) (any, error) {
	var features uint32
	if v, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap); ok {
		features, _ = v.(uint32)
	}
	if err := spec.AdmitRequest(ctx, spec.Lookup(srv.MatterClusterID()), features, cmd, fields, srv.MatterRead); err != nil {
		return nil, err
	}
	return srv.MatterInvoke(ctx, cmd, fields)
}
