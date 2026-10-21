// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"testing"

	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// TestServiceAreaOrigins is the origin audit of the ServiceArea server:
// the device's own CurrentArea is OriginHostSet, a SelectAreas
// OriginCommand, a controller's write of the read-only SelectedAreas
// nothing (the cluster has no writable attribute).
func TestServiceAreaOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		return newServer(t, vacuum(&device{}))
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := sa.Wrap(srv).Set(sa.Patch{CurrentArea: ptr(uint32(7))}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if _, err := srv.MatterInvoke(context.Background(), sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7}}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), sa.AttrSelectedAreas, []uint32{7}); err == nil {
				tb.Fatal("a write of the read-only SelectedAreas succeeded")
			}
		}},
	)
}
