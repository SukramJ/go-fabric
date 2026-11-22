// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"slices"
	"testing"

	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/contract"
)

// TestThermostatAtomicWritesAreFabricScopedState (R2-P5-apidev-09): the
// atomic writes a fabric's peers hold go through contract.FabricScopedState,
// the one removal path the node calls on every server — MatterHeldFabrics
// names the fabric whose peer holds a write, MatterDropFabric closes it.
func TestThermostatAtomicWritesAreFabricScopedState(t *testing.T) {
	t.Parallel()
	cfg := thermostatConfig(5, []thermo.Preset{})
	cfg.PresetStore = &memoryPresetStore{}
	srv := mustNew(t, cfg)
	var state contract.FabricScopedState = srv
	if held := state.MatterHeldFabrics(); len(held) != 0 {
		t.Fatalf("held fabrics %v before any write, want none", held)
	}
	peer, other := remoteCtx(1, 5), remoteCtx(2, 5)
	begin := func(ctx context.Context) (tdef.AtomicResponse, error) {
		r, err := srv.MatterInvoke(ctx, tdef.CmdAtomicRequest, tdef.AtomicRequestRequest{
			RequestType: beginWrite, AttributeRequests: []uint32{presetsAttribute}, Timeout: timeoutMs(5000),
		})
		if err != nil {
			return tdef.AtomicResponse{}, err
		}
		return r.(tdef.AtomicResponse), nil
	}
	if _, err := begin(peer); err != nil {
		t.Fatal(err)
	}
	if held := state.MatterHeldFabrics(); !slices.Equal(held, []uint8{1}) {
		t.Fatalf("held fabrics %v with fabric 1's write open, want [1]", held)
	}
	if err := state.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if held := state.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("held fabrics %v after the drop, want none", held)
	}
	if r, err := begin(other); err != nil || r.StatusCode != tdef.StatusSuccess {
		t.Errorf("BeginWrite after fabric 1 was dropped %+v, %v, want SUCCESS", r, err)
	}
}
