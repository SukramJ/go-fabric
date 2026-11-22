// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	valvedef "thingfabric.dev/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"thingfabric.dev/go-fabric/cluster/valve"
)

// TestEmitEventOnTheMountedEndpoint (R2-P5-apispec-07): EmitEvent — the
// typed Record<Event> — answers ErrNotMounted until the bridge gives the
// server its emitter and endpoint, then emits there.
func TestEmitEventOnTheMountedEndpoint(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost(), Events: []uint32{valvedef.EventValveFault}})
	if err := srv.EmitEvent(valvedef.EventValveFault, valvedef.ValveFaultEvent{}); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("EmitEvent before mounting: %v, want ErrNotMounted", err)
	}
	log := &eventLog{}
	srv.SetMatterEventEmitter(log)
	srv.SetEndpoint(6)
	if err := valvedef.Wrap(srv).RecordValveFault(valvedef.ValveFaultEvent{}); err != nil {
		t.Fatal(err)
	}
	if got := log.all(); len(got) != 1 || got[0].endpoint != 6 {
		t.Errorf("emitted %+v, want one ValveFault on endpoint 6", got)
	}
}

// TestDefaultOpenLevelVetoHearsAChangeOnly (R2-P5-apispec-02): the held
// DefaultOpenLevel reaches no veto, a change does and is refused.
func TestDefaultOpenLevelVetoHearsAChangeOnly(t *testing.T) {
	t.Parallel()
	host := newLevelHost()
	srv := valve.NewServer(valve.Config{Source: host, Features: valve.FeatureLevel, Attributes: []uint32{valvedef.AttrDefaultOpenLevel}})
	refusal := errors.New("no")
	calls := 0
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { calls++; return refusal })
	held := uint64(host.DefaultOpenLevel())
	if err := srv.MatterWrite(context.Background(), valvedef.AttrDefaultOpenLevel, held); err != nil || calls != 0 {
		t.Errorf("held level: %v, %d veto calls", err, calls)
	}
	next := uint64(50)
	if next == held {
		next = 60
	}
	if err := srv.MatterWrite(context.Background(), valvedef.AttrDefaultOpenLevel, next); !errors.Is(err, refusal) {
		t.Errorf("a change: %v, want the veto", err)
	}
}
