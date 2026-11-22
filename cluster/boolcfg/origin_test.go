// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/boolcfg"
	bcdef "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
	"thingfabric.dev/go-fabric/contract"
)

// TestBooleanStateConfigurationWriteOrigins is the controller-write row of
// the origin audit of a server that delegates its writes to spec.Server
// (brief BY-1 §2): the write of CurrentSensitivityLevel reaches the
// change listeners through the wrapper as the written attribute.
func TestBooleanStateConfigurationWriteOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		srv, err := boolcfg.New(boolcfg.Config{Features: boolcfg.FeatureSensitivityLevel, SupportedSensitivityLevels: 3})
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.Write(bcdef.AttrCurrentSensitivityLevel, uint64(2)),
	)
}
