// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/basic-information/CapabilityMinimaTest.ts
// at 862ad75f: the four Matter 1.6 CapabilityMinima fields
// BasicInformationServer.initialize seeds (BasicInformationServer.ts:26-30,
// :73-83). "seeds the four 1.6 fields with the advertised defaults" is
// TestCapabilityMinimaRev6Defaults (basic_information_test.go).
//
// matter.js configures the root's basicInformation state on a
// MockServerNode and reads it back with stateOf. The Go state is
// core.Config.CapabilityMinima, which node.Config does not carry (the
// facade serves the defaults), so the port builds the server with it and
// reads the attribute through MatterRead.
package core_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestCapabilityMinimaPreservesCallerValues ports "preserves caller-provided
// values over the derived defaults" (CapabilityMinimaTest.ts:25-43): with
// readPathsSupported 12 and subscribePathsSupported 5 given, they stay, and
// the two absent fields take the default 20.
func TestCapabilityMinimaPreservesCallerValues(t *testing.T) {
	t.Parallel()
	cfg := validBasicInfoConfig()
	cfg.CapabilityMinima = core.CapabilityMinimaStruct{
		CaseSessionsPerFabric:   3,
		SubscriptionsPerFabric:  3,
		ReadPathsSupported:      12,
		SubscribePathsSupported: 5,
	}
	b, err := core.NewBasicInformation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := b.MatterRead(0x0013) // CapabilityMinima
	cm, ok := v.(core.CapabilityMinimaStruct)
	if !ok {
		t.Fatalf("CapabilityMinima = %T", v)
	}
	if cm.ReadPathsSupported != 12 || cm.SubscribePathsSupported != 5 {
		t.Errorf("read/subscribe paths = %d/%d, want 12/5", cm.ReadPathsSupported, cm.SubscribePathsSupported)
	}
	if cm.SimultaneousInvocationsSupported != 20 || cm.SimultaneousWritesSupported != 20 {
		t.Errorf("invocations/writes = %d/%d, want 20/20", cm.SimultaneousInvocationsSupported, cm.SimultaneousWritesSupported)
	}
}
