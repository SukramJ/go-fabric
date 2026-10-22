// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// TestOnOffWriteOrigins is the controller-write rows of the OnOff
// server's origin audit (brief BY-1 §2): each writable attribute of the
// Lighting feature reaches the change listeners — the host's OnChanged —
// as the written attribute, from the write (Datasource.ts:1473-1476).
func TestOnOffWriteOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.Write(onoff.AttrOnTime, uint64(30)),
		spectest.Write(onoff.AttrOffWaitTime, uint64(30)),
		spectest.Write(onoff.AttrStartUpOnOff, uint64(1)),
	)
}
