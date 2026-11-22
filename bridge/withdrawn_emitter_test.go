// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	bsdef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
)

// TestRemovedServerEmitsIntoNoEventLog (R2-P5-apispec-07 = R2-P5-apidev-07):
// a spec.Server mounted on endpoint 7 learns its endpoint and emits its
// event there; after a reassembly that drops the endpoint the bridge has
// withdrawn its emitter, so EmitEvent answers ErrNotMounted, the deprecated
// Emit drops the event, and the event log stays as it was. A reassembly that brings it back gives the emitter back.
func TestRemovedServerEmitsIntoNoEventLog(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(bsdef.Definition, spec.Options{Features: uint32(bsdef.FeatureChangeEvent)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	snap := newSwitchedSnapshotter(srv)
	b := newStartedBridgeWithSnapshotter(t, snap.snapshot)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	logged := func() int { return len(b.eventLog.Query(7, bsdef.ClusterID, bsdef.EventStateChange, 0)) }

	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: true}); err != nil {
		t.Fatalf("EmitEvent on the mounted server: %v", err)
	}
	if n := logged(); n != 1 {
		t.Fatalf("event log holds %d events of endpoint 7 after the emit, want 1", n)
	}

	snap.set(false, 0)
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("the Reassemble removing the server: %v", err)
	}
	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: false}); !errors.Is(err, spec.ErrNotMounted) {
		t.Fatalf("EmitEvent on the removed server: %v, want ErrNotMounted", err)
	}
	if err := srv.Emit(7, bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil { //nolint:staticcheck // the deprecated Emit keeps its behaviour: dropped, nil
		t.Fatalf("Emit on the removed server: %v", err)
	}
	if n := logged(); n != 1 {
		t.Fatalf("event log holds %d events after the removed server emitted, want 1", n)
	}

	snap.set(true, 0)
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("the Reassemble adding the server back: %v", err)
	}
	if err := srv.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{StateValue: true}); err != nil {
		t.Fatal(err)
	}
	if n := logged(); n != 2 {
		t.Fatalf("event log holds %d events after the server came back and emitted, want 2", n)
	}
}
