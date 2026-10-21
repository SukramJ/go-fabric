// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/spec"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestTypedPumpServer drives pump.Server through its typed server: Get
// reads the StateSource and the limits, Set refuses (the pump holds no
// attribute of its own), a typed event reaches the emitter and an
// undeclared one is refused, a handler is refused (no commands), and
// OnWrite vetoes a controller write before the setter sees it.
func TestTypedPumpServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{st: pump.State{OperationMode: pump.OperationNormal}}
	srv := newFull(t, d)
	srv.SetEndpoint(3)
	s := pumpdef.Wrap(srv)

	if st := s.Get(); st.MaxPressure == nil || *st.MaxPressure != 1000 || st.OperationMode != pumpdef.OperationModeNormal {
		t.Errorf("state %+v", st)
	}
	mode := pumpdef.OperationModeMaximum
	if err := s.Set(pumpdef.Patch{OperationMode: &mode}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set OperationMode: %v, want ErrNotHeld", err)
	}
	if err := s.Set(pumpdef.Patch{}); err != nil {
		t.Errorf("an empty patch: %v", err)
	}

	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	if err := s.EmitDryRunning(srv.Endpoint(), pumpdef.DryRunningEvent{}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 || rec.events[0].endpoint != 3 || rec.events[0].event != pump.EventDryRunning ||
		rec.events[0].data != (clusterwire.FieldlessEvent{}) || rec.events[0].priority != contract.EventPriorityCritical {
		t.Errorf("emitted %+v", rec.events)
	}
	if err := s.EmitPumpBlocked(3, pumpdef.PumpBlockedEvent{}); !errors.Is(err, spec.ErrNotEmitted) || !errors.Is(err, pump.ErrEventUndeclared) {
		t.Errorf("an undeclared event: %v", err)
	}

	if err := spec.HandleOn(srv, 0x00, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a handler: %v, want ErrNotAccepted", err)
	}

	refusal := spec.Errorf(im.StatusConstraintError, "the pump is locked")
	var seen []pumpdef.State
	if err := s.OnWrite(func(_ context.Context, next pumpdef.State, attrID uint32) error {
		seen = append(seen, next)
		if attrID == pump.AttrOperationMode && next.OperationMode == pumpdef.OperationModeMaximum {
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(pump.OperationMaximum)); !errors.Is(err, refusal) {
		t.Errorf("a vetoed write: %v", err)
	}
	if d.st.OperationMode != pump.OperationNormal {
		t.Error("a vetoed write reached the setter")
	}
	if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(pump.OperationMinimum)); err != nil || d.st.OperationMode != pump.OperationMinimum {
		t.Errorf("an accepted write: %v, mode %d", err, d.st.OperationMode)
	}
	if len(seen) != 2 || seen[0].OperationMode != pumpdef.OperationModeMaximum {
		t.Errorf("the veto saw %+v", seen)
	}
}
