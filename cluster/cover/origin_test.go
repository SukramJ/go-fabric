// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/contract"
)

// TestWindowCoveringWriteOrigins is the controller-write row of the
// WindowCovering server's origin audit (brief BY-1 §2): a write of Mode
// reaches the change listeners — the host's OnChanged — as Mode, from the
// write (Datasource.ts:1473-1476).
func TestWindowCoveringWriteOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		srv, err := cover.New(cover.Config{FeatureMap: uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)})
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.Write(wcdef.AttrMode, uint64(1)), // MotorDirectionReversed
	)
}
