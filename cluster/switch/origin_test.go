// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package switchserver

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/contract"
)

// TestSwitchOrigins is the origin audit of the Switch server: the host's
// write of the debounced CurrentPosition (a typed Set) is OriginHostSet, a
// controller's write of the read-only CurrentPosition nothing. No command
// row: the cluster defines no command.
func TestSwitchOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		s, err := New(Config{Features: FeatureLS})
		if err != nil {
			tb.Fatal(err)
		}
		return s
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			one := uint8(1)
			if err := swdef.Wrap(srv).Set(swdef.Patch{CurrentPosition: &one}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), swdef.AttrCurrentPosition, uint8(1)); err == nil {
				tb.Fatal("a write of the read-only CurrentPosition succeeded")
			}
		}},
	)
}
