// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"slices"
	"testing"

	tndef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// threadDiagAttributes builds a ThreadBorderRouter whose
// ThreadNetworkDiagnostics declaration is decl and returns the cluster's
// AttributeList.
func threadDiagAttributes(t *testing.T, key string, decl tndef.Declare) []uint32 {
	t.Helper()
	d, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey(key), Name: key}, decl)
	if err != nil {
		t.Fatal(err)
	}
	lister, ok := d.Server(tndef.ClusterID).(contract.ClusterAttributeLister)
	if !ok {
		t.Fatal("the ThreadNetworkDiagnostics server lists no attributes")
	}
	return lister.MatterAttributes()
}

// TestParityMatterJS_ThreadNetworkDiagnosticsProvisional ports
// packages/node/test/behaviors/thread-network-diagnostics/
// ThreadNetworkDiagnosticsProvisionalTest.ts at 862ad75f (:11-36):
// ExtAddress (0x3F) and Rloc16 (0x40) are provisional ("P, M") in Matter
// 1.6, so they are absent until the application supplies a value — here a
// declaration's Initial, null as in the matter.js case (:32). The
// provisional term makes the mandatory one optional (spec.Conformance's
// applicability, after packages/model/src/aspects/Conformance.ts), and an
// optional attribute with a value is served (ValidatedElements.ts:251-252).
func TestParityMatterJS_ThreadNetworkDiagnosticsProvisional(t *testing.T) {
	t.Parallel()
	t.Run("are absent by default", func(t *testing.T) {
		t.Parallel()
		ids := threadDiagAttributes(t, "tnd-default", tndef.Declare{})
		if slices.Contains(ids, tndef.AttrExtAddress) || slices.Contains(ids, tndef.AttrRloc16) {
			t.Errorf("AttributeList %v holds ExtAddress or Rloc16", ids)
		}
	})
	t.Run("are present once the application supplies a value", func(t *testing.T) {
		t.Parallel()
		ids := threadDiagAttributes(t, "tnd-supplied", tndef.Declare{
			Initial: tndef.Patch{Null: []uint32{tndef.AttrExtAddress, tndef.AttrRloc16}},
		})
		if !slices.Contains(ids, tndef.AttrExtAddress) || !slices.Contains(ids, tndef.AttrRloc16) {
			t.Errorf("AttributeList %v misses ExtAddress or Rloc16", ids)
		}
	})
}
