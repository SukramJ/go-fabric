// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	bsdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// stateEvents records the StateChange events emitted.
type stateEvents struct {
	endpoints []uint16
	values    []bool
}

func (e *stateEvents) MatterEmitEvent(endpoint uint16, _, _ uint32, data any, _ contract.EventPriority) {
	ev, _ := data.(bsdef.StateChangeEvent)
	e.endpoints, e.values = append(e.endpoints, endpoint), append(e.values, ev.StateValue)
}

// TestHeldBooleanStateEmitsStateChange: with ChangeEvent every change of
// StateValue the host sets emits StateChange with the new value on the
// stamped endpoint, as matter.js's BooleanStateServer does; an unchanged
// value emits nothing, and without the feature nothing is emitted.
func TestHeldBooleanStateEmitsStateChange(t *testing.T) {
	t.Parallel()
	srv, err := measurement.NewHeldBooleanState(measurement.HeldBooleanStateConfig{Features: bsdef.FeatureChangeEvent})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, bsdef.Definition, uint32(bsdef.FeatureChangeEvent))
	ev := &stateEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(7)
	typed := bsdef.Wrap(srv)
	for _, v := range []bool{true, true, false} {
		if err := typed.Set(bsdef.Patch{StateValue: &v}); err != nil {
			t.Fatal(err)
		}
	}
	if len(ev.values) != 2 || !ev.values[0] || ev.values[1] || ev.endpoints[0] != 7 {
		t.Errorf("events %+v, want true then false on endpoint 7", ev)
	}

	plain, err := measurement.NewHeldBooleanState(measurement.HeldBooleanStateConfig{})
	if err != nil {
		t.Fatal(err)
	}
	quiet := &stateEvents{}
	plain.SetMatterEventEmitter(quiet)
	on := true
	if err := bsdef.Wrap(plain).Set(bsdef.Patch{StateValue: &on}); err != nil {
		t.Fatal(err)
	}
	if len(quiet.values) != 0 {
		t.Errorf("events %+v without ChangeEvent, want none", quiet)
	}
}

// TestHeldBooleanStateOrigin is the origin audit: the host's Set is
// OriginHostSet; StateValue is read-only to a controller.
func TestHeldBooleanStateOrigin(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()

		srv, err := measurement.NewHeldBooleanState(measurement.HeldBooleanStateConfig{Features: bsdef.FeatureChangeEvent})
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			on := true
			if err := bsdef.Wrap(srv).Set(bsdef.Patch{StateValue: &on}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), bsdef.AttrStateValue, true); err == nil {
				tb.Fatal("a write of the read-only StateValue succeeded")
			}
		}},
	)
}
