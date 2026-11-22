// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bsdef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	"thingfabric.dev/go-fabric/contract"
)

// TestEmitEventNeedsTheEndpoint: EmitEvent before the server knows its
// endpoint is ErrNotMounted and emits nothing — never on endpoint 0; after
// SetEndpoint it emits on that endpoint, and the generated typed
// Record<Event> goes the same way; an event outside EventList stays
// ErrNotEmitted; after the emitter is withdrawn it is ErrNotMounted again
// (R2-P5-apispec-07, coordinator round 2 Q5).
func TestEmitEventNeedsTheEndpoint(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(bsdef.Definition, spec.Options{Features: uint32(bsdef.FeatureChangeEvent)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	typed := bsdef.Wrap(srv)
	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: true}); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("EmitEvent before SetEndpoint: %v, want ErrNotMounted", err)
	}
	if err := typed.RecordStateChange(bsdef.StateChangeEvent{StateValue: true}); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("RecordStateChange before SetEndpoint: %v, want ErrNotMounted", err)
	}
	if len(em.events) != 0 {
		t.Fatalf("emitted before the endpoint was known: %+v", em.events)
	}
	var _ contract.EndpointReceiver = srv
	srv.SetEndpoint(4)
	if err := typed.RecordStateChange(bsdef.StateChangeEvent{StateValue: true}); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != 1 || em.events[0].endpoint != 4 || em.events[0].event != bsdef.EventStateChange {
		t.Fatalf("emitted %+v, want StateChange on endpoint 4", em.events)
	}
	if err := srv.EmitEvent(0x7F, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event outside EventList: %v", err)
	}
	held := spec.NewSelfHeld(srv)
	if err := held.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil || len(em.events) != 2 {
		t.Errorf("SelfHeld.EmitEvent: %v, %d events", err, len(em.events))
	}
	// Withdrawn (the server left the topology): ErrNotMounted, nothing
	// emitted; the deprecated Emit keeps dropping it.
	srv.SetMatterEventEmitter(nil)
	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{}); !errors.Is(err, spec.ErrNotMounted) {
		t.Errorf("EmitEvent after the emitter was withdrawn: %v, want ErrNotMounted", err)
	} else if !strings.Contains(err.Error(), "no event emitter") {
		// The text names this cause too, not only the unknown endpoint
		// (review II, phase 5b).
		t.Errorf("EmitEvent after the emitter was withdrawn: %q does not name the missing emitter", err)
	}
	if err := srv.Emit(4, bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil { //nolint:staticcheck // the deprecated Emit's behaviour
		t.Errorf("Emit after the emitter was withdrawn: %v", err)
	}
	srv.SetMatterEventEmitter(em)
	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil || len(em.events) != 3 {
		t.Errorf("EmitEvent with the emitter back: %v, %d events", err, len(em.events))
	}
}
