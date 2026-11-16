// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"context"
	"sync"
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

// lockedEvents records StateChange payloads from concurrent sets.
type lockedEvents struct {
	mu     sync.Mutex
	values []bool
}

func (e *lockedEvents) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	ev, _ := data.(bsdef.StateChangeEvent)
	e.mu.Lock()
	e.values = append(e.values, ev.StateValue)
	e.mu.Unlock()
}

// TestHeldBooleanStateEventCarriesItsChange holds R2-P6-conc-1462: each
// StateChange carries the value of the change it reports, also when two
// host goroutines set StateValue at once — only a change emits, so the
// payloads alternate; two equal payloads in a row are one change reported
// twice with a value read after the next one.
func TestHeldBooleanStateEventCarriesItsChange(t *testing.T) {
	t.Parallel()
	srv, err := measurement.NewHeldBooleanState(measurement.HeldBooleanStateConfig{Features: bsdef.FeatureChangeEvent})
	if err != nil {
		t.Fatal(err)
	}
	ev := &lockedEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(1)
	typed := bsdef.Wrap(srv)
	var wg sync.WaitGroup
	for _, v := range []bool{true, false} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				_ = typed.Set(bsdef.Patch{StateValue: &v})
			}
		}()
	}
	wg.Wait()
	ev.mu.Lock()
	defer ev.mu.Unlock()
	for i := 1; i < len(ev.values); i++ {
		if ev.values[i] == ev.values[i-1] {
			t.Fatalf("events %d and %d both carry %v: a change reported with another change's value", i-1, i, ev.values[i])
		}
	}
}
