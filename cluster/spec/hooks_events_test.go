// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bsdef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
)

// TestHooksEmitEventOnTheMountedEndpoint (R2-P5-apispec-07): a server over
// spec.Hooks emits through EmitEvent — the typed Record<Event> — once it
// has an emitter and an endpoint; ErrNotMounted before, ErrNotEmitted for
// an event outside EventList.
func TestHooksEmitEventOnTheMountedEndpoint(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(bsdef.Definition, spec.Options{Features: uint32(bsdef.FeatureChangeEvent)})
	if err != nil {
		t.Fatal(err)
	}
	var h spec.Hooks
	h.Init(inst)
	if err := h.EmitEvent(bsdef.EventStateChange, nil); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("before mounting: %v, want ErrNotMounted", err)
	}
	em := &emitter{}
	h.SetMatterEventEmitter(em)
	if err := h.EmitEvent(bsdef.EventStateChange, nil); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("an emitter without an endpoint: %v, want ErrNotMounted", err)
	}
	h.SetEndpoint(2)
	if err := h.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil || len(em.events) != 1 || em.events[0].endpoint != 2 {
		t.Fatalf("mounted: %v, %+v", err, em.events)
	}
	if err := h.EmitEvent(0x7F, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event outside EventList: %v", err)
	}
}
