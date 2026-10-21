// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	"github.com/SukramJ/go-fabric/contract"
)

// originOf records the origin of every change the server reports.
type originOf struct{ seen []contract.Change }

func (o *originOf) record(c contract.Change) { o.seen = append(o.seen, c) }

// TestTheChangesOrigin is the origin audit of the OperationalState server:
// a host's Set reports OriginHostSet, a change the handler of a command
// makes reports OriginCommand, and a controller's write changes nothing
// (every attribute is read-only).
func TestTheChangesOrigin(t *testing.T) {
	t.Parallel()
	running, paused := opdef.OperationalStateRunning, opdef.OperationalStatePaused
	for _, c := range []struct {
		name string
		act  func(t *testing.T, srv *opstate.Server)
		want []contract.Change
	}{
		{"host set", func(t *testing.T, srv *opstate.Server) {
			t.Helper()
			if err := opdef.Wrap(srv).Set(opdef.Patch{OperationalState: &running}); err != nil {
				t.Fatal(err)
			}
		}, []contract.Change{{Attributes: []uint32{opdef.AttrOperationalState}, Origin: contract.OriginHostSet}}},
		{"command", func(t *testing.T, srv *opstate.Server) {
			t.Helper()
			if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
				t.Fatal(err)
			}
			if _, err := srv.MatterInvoke(context.Background(), opdef.CmdPause, nil); err != nil {
				t.Fatal(err)
			}
		}, []contract.Change{
			{Attributes: []uint32{opdef.AttrOperationalState}, Origin: contract.OriginHostSet},
			{Attributes: []uint32{opdef.AttrOperationalState}, Origin: contract.OriginCommand},
		}},
		{"controller write", func(t *testing.T, srv *opstate.Server) {
			t.Helper()
			if err := srv.MatterWrite(context.Background(), opdef.AttrOperationalState, uint8(paused)); err == nil {
				t.Fatal("a write of the read-only OperationalState succeeded")
			}
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := &device{follow: map[opstate.Command]opstate.State{opstate.CommandPause: opstate.StatePaused}}
			srv := newWasher(t, d, nil)
			var o originOf
			srv.OnAttributeChange(o.record)
			c.act(t, srv)
			if len(o.seen) != len(c.want) {
				t.Fatalf("changes %+v, want %+v", o.seen, c.want)
			}
			for i, w := range c.want {
				if got := o.seen[i]; got.Origin != w.Origin || len(got.Attributes) != len(w.Attributes) || got.Attributes[0] != w.Attributes[0] {
					t.Errorf("change %d: %+v, want %+v", i, got, w)
				}
			}
		})
	}
}

// TestTypedServer drives the server through the generated typed form: a
// Set of every settable attribute, all or nothing; the state read back;
// the events; a host handler over the server's own command logic.
func TestTypedServer(t *testing.T) {
	t.Parallel()
	d := &device{}
	rec := &recorder{}
	srv := newWasher(t, d, func(cfg *opstate.Config) {
		cfg.Phases, cfg.CountdownTime, cfg.OperationCompletion = []string{"wash", "rinse"}, true, true
		zero := uint8(0)
		cfg.CurrentPhase = &zero
	})
	srv.SetMatterEventEmitter(rec)
	typed := opdef.Wrap(srv)

	one, sixty := uint8(1), uint32(60)
	label := "lid open"
	failure := opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateUnableToStartOrResume, ErrorStateDetails: &label}
	if err := typed.Set(opdef.Patch{CurrentPhase: &one, CountdownTime: &sixty, OperationalError: &failure}); err != nil {
		t.Fatal(err)
	}
	st := typed.Get()
	if st.OperationalState != opdef.OperationalStateError || st.CurrentPhase == nil || *st.CurrentPhase != 1 ||
		st.CountdownTime == nil || *st.CountdownTime != 60 || st.OperationalError.ErrorStateId != opdef.ErrorStateUnableToStartOrResume ||
		st.OperationalError.ErrorStateDetails == nil || *st.OperationalError.ErrorStateDetails != label {
		t.Errorf("state after the Set: %+v", st)
	}
	if evs := rec.all(); len(evs) != 1 || evs[0].event != opdef.EventOperationalError {
		t.Errorf("events after an error: %+v, want one OperationalError", evs)
	}

	// All or nothing: a phase outside the list refuses the whole Set.
	five, stopped := uint8(5), opdef.OperationalStateStopped
	if err := typed.Set(opdef.Patch{CurrentPhase: &five, OperationalState: &stopped}); !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("Set with an out-of-list phase: %v, want ErrCurrentPhase", err)
	}
	if typed.Get().OperationalState != opdef.OperationalStateError {
		t.Error("a refused Set changed OperationalState")
	}
	list := []opdef.OperationalStateStruct{{OperationalStateId: opdef.OperationalStateStopped}}
	if err := typed.Set(opdef.Patch{OperationalStateList: &list}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of OperationalStateList: %v, want ErrNotHeld", err)
	}
	if err := typed.Set(opdef.Patch{Null: []uint32{opdef.AttrPhaseList}}); err != nil {
		t.Errorf("Set PhaseList null: %v", err)
	}
	if st := typed.Get(); st.PhaseList != nil || st.CurrentPhase != nil {
		t.Errorf("after a null PhaseList: %+v, want both null", st)
	}

	// Events.
	if err := typed.EmitOperationCompletion(3, opdef.OperationCompletionEvent{CompletionErrorCode: uint8(opdef.ErrorStateNoError)}); err != nil {
		t.Errorf("EmitOperationCompletion: %v", err)
	}
	if evs := rec.all(); len(evs) != 2 || evs[1].event != opdef.EventOperationCompletion || evs[1].endpoint != 3 {
		t.Errorf("events: %+v, want an OperationCompletion on endpoint 3", evs)
	}
	if err := typed.EmitOperationCompletion(3, opdef.OperationCompletionEvent{CompletionErrorCode: 0x7F}); !errors.Is(err, opstate.ErrUnknownError) {
		t.Errorf("EmitOperationCompletion with an unknown code: %v, want ErrUnknownError", err)
	}

	// A host handler takes the command over, the server's state check
	// included.
	if err := typed.HandlePause(func(context.Context, opdef.PauseRequest) (opdef.OperationalCommandResponse, error) {
		return opdef.OperationalCommandResponse{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), opdef.CmdPause, opdef.PauseRequest{}); err != nil {
		t.Errorf("Pause through the host handler: %v", err)
	}
	if len(d.commands()) != 0 {
		t.Errorf("the server's handler saw %v, want nothing", d.commands())
	}
	if err := typed.OnWrite(func(context.Context, opdef.State, uint32) error { return nil }); err != nil {
		t.Errorf("OnWrite: %v", err)
	}
}
