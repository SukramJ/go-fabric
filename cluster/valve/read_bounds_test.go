// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"testing"

	valvedef "thingfabric.dev/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"thingfabric.dev/go-fabric/cluster/valve"
)

// TestReadHoldsHostValuesToTheAttribute (R2-C2-wire-1553): a host level
// above 100 reads 100, a DefaultOpenLevel outside 1..100 reads the nearer
// bound, and a ValveState outside the enum reads the last defined one the
// host reported (null before any).
func TestReadHoldsHostValuesToTheAttribute(t *testing.T) {
	t.Parallel()
	h := newLevelHost()
	h.current, h.target, h.targetKnown, h.defaultLevel = 150, 200, true, 0
	h.currentState, h.targetState = 5, 7
	srv := valve.NewServer(valve.Config{
		Source: h, Features: valve.FeatureLevel, Attributes: []uint32{valvedef.AttrDefaultOpenLevel},
	})
	read := func(id uint32) any {
		v, _ := srv.MatterRead(id)
		return v
	}
	for id, want := range map[uint32]any{
		valvedef.AttrCurrentLevel: uint8(100), valvedef.AttrTargetLevel: uint8(100), valvedef.AttrDefaultOpenLevel: uint8(1),
		valvedef.AttrCurrentState: nil, valvedef.AttrTargetState: nil,
	} {
		if v := read(id); v != want {
			t.Errorf("attribute 0x%X reads %v before any defined state, want %v", id, v, want)
		}
	}
	h.currentState, h.targetState = valve.StateOpen, valve.StateTransitioning
	_, _ = read(valvedef.AttrCurrentState), read(valvedef.AttrTargetState)
	h.currentState, h.targetState = 5, 7
	if v := read(valvedef.AttrCurrentState); v != uint8(valve.StateOpen) {
		t.Errorf("CurrentState reads %v after an undefined state, want the last defined %d", v, valve.StateOpen)
	}
	if v := read(valvedef.AttrTargetState); v != uint8(valve.StateTransitioning) {
		t.Errorf("TargetState reads %v after an undefined state, want the last defined %d", v, valve.StateTransitioning)
	}
}
