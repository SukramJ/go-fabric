// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	"github.com/SukramJ/go-fabric/im"
)

// phasedWasher is a washer with two phases, a countdown and the
// OperationCompletion event.
func phasedWasher(t *testing.T) (*opstate.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := newWasher(t, &device{}, func(cfg *opstate.Config) {
		cfg.Phases, cfg.CountdownTime, cfg.OperationCompletion = []string{"wash", "rinse"}, true, true
		zero := uint8(0)
		cfg.CurrentPhase = &zero
	})
	srv.SetMatterEventEmitter(rec)
	return srv, rec
}

// TestSetAttributesRefusesWhatTheDefinitionOrTheRulesRefuse: every value
// is held to the definition and to the rules of its setter before any is
// applied, so a refusal changes nothing.
func TestSetAttributesRefusesWhatTheDefinitionOrTheRulesRefuse(t *testing.T) {
	t.Parallel()

	badUTF8 := "\xff"
	nilID := struct {
		ErrorStateId      *uint8 //nolint:revive // the generated types spell an id "Id"
		ErrorStateLabel   *string
		ErrorStateDetails *string
	}{}
	cases := []struct {
		name   string
		values map[uint32]any
		want   error
	}{
		{"an attribute the server does not serve", map[uint32]any{0xFFF0: uint8(1)}, spec.ErrNotServed},
		{"a state of the wrong type", map[uint32]any{opdef.AttrOperationalState: "running"}, spec.ErrInvalidValue},
		{"a state outside the list", map[uint32]any{opdef.AttrOperationalState: uint8(0x81)}, opstate.ErrStateNotListed},
		{"an error state without an ID", map[uint32]any{opdef.AttrOperationalError: nilID}, spec.ErrInvalidValue},
		{"an error state with an unlisted ID", map[uint32]any{opdef.AttrOperationalError: opdef.ErrorStateStruct{ErrorStateId: 0x7F}}, spec.ErrInvalidValue},
		{"an error state with an invalid label", map[uint32]any{opdef.AttrOperationalError: opdef.ErrorStateStruct{
			ErrorStateId: opdef.ErrorStateUnableToStartOrResume, ErrorStateDetails: &badUTF8,
		}}, opstate.ErrTooLong},
		{"a phase list as an array", map[uint32]any{opdef.AttrPhaseList: [1]string{"wash"}}, spec.ErrInvalidValue},
		{"a phase list a phase beyond the current one", map[uint32]any{
			opdef.AttrPhaseList: []string{"a"}, opdef.AttrCurrentPhase: uint8(3),
		}, opstate.ErrCurrentPhase},
		{"a phase outside the list", map[uint32]any{opdef.AttrCurrentPhase: uint8(9)}, opstate.ErrCurrentPhase},
		{"a countdown beyond the maximum", map[uint32]any{opdef.AttrCountdownTime: opstate.CountdownTimeMax + 1}, nil},
		{"the configured list", map[uint32]any{opdef.AttrOperationalStateList: []opdef.OperationalStateStruct{{}}}, spec.ErrNotHeld},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv, rec := phasedWasher(t)
			before := srv.MatterDataVersion()
			err := srv.SetAttributes(c.values)
			switch {
			case err == nil:
				t.Fatal("SetAttributes succeeded")
			case c.want != nil && !errors.Is(err, c.want):
				t.Fatalf("SetAttributes = %v, want %v", err, c.want)
			}
			if srv.MatterDataVersion() != before || len(rec.all()) != 0 {
				t.Error("a refused Set changed something")
			}
		})
	}
}

// TestSetAttributesPhaseListWithoutAPhase: a new list needs a phase to
// start on, and the current one is null when the server has none.
func TestSetAttributesPhaseListWithoutAPhase(t *testing.T) {
	t.Parallel()

	plain := newWasher(t, &device{}, nil)
	err := plain.SetAttributes(map[uint32]any{opdef.AttrPhaseList: []string{"a"}})
	if !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("a phase list without a current phase: %v, want ErrCurrentPhase", err)
	}
	// Both together are accepted; an any-typed list is a list of strings.
	if err := plain.SetAttributes(map[uint32]any{opdef.AttrPhaseList: []any{"a", "b"}, opdef.AttrCurrentPhase: uint8(1)}); err != nil {
		t.Errorf("SetAttributes(any list) = %v", err)
	}
	if v, _ := plain.MatterRead(opdef.AttrCurrentPhase); v != uint8(1) {
		t.Errorf("CurrentPhase %v, want 1", v)
	}
}

// TestSetAttributesOvenCavityHoldsThePhasesToTheCluster: the oven
// cavity's PhaseList is limited to the phases the cluster names, a
// restriction the Set applies as the initialisation does.
func TestSetAttributesOvenCavityHoldsThePhasesToTheCluster(t *testing.T) {
	t.Parallel()

	srv, err := opstate.NewOvenCavityServer(opstate.Config{
		Handler: &device{}, States: washerStates, State: opstate.StateStopped,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = srv.SetAttributes(map[uint32]any{opdef.AttrPhaseList: []string{"baking"}, opdef.AttrCurrentPhase: uint8(0)})
	if !errors.Is(err, opstate.ErrPhaseNotAllowed) {
		t.Errorf("a phase outside the cluster's: %v, want ErrPhaseNotAllowed", err)
	}
	err = srv.SetAttributes(map[uint32]any{opdef.AttrPhaseList: opstate.OvenCavityPhases, opdef.AttrCurrentPhase: uint8(1)})
	if err != nil {
		t.Errorf("the cluster's own phases: %v", err)
	}
}

// TestEmitRefusesWhatTheEventListDoesNot: an event outside the EventList
// is refused, a completion needs one of the cluster's error codes, and a
// served event reaches the emitter or is dropped without one.
func TestEmitRefusesWhatTheEventListDoesNot(t *testing.T) {
	t.Parallel()

	plain := newWasher(t, &device{}, nil)
	if err := plain.Emit(1, opdef.EventOperationCompletion, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit(OperationCompletion) on a server without it = %v, want ErrNotEmitted", err)
	}

	srv, rec := phasedWasher(t)
	srv.SetMatterEventEmitter(nil)
	// A payload without a completion code is not held to the error states,
	// and without an emitter it is dropped.
	for _, data := range []any{nil, struct{ Other uint8 }{}, &struct{ CompletionErrorCode *uint8 }{}} {
		if err := srv.Emit(1, opdef.EventOperationCompletion, data); err != nil {
			t.Errorf("Emit(%T) = %v", data, err)
		}
	}
	srv.SetMatterEventEmitter(rec)
	code := uint8(opdef.ErrorStateNoError)
	if err := srv.Emit(2, opdef.EventOperationCompletion, &struct{ CompletionErrorCode *uint8 }{&code}); err != nil {
		t.Fatalf("Emit(pointer payload) = %v", err)
	}
	if evs := rec.all(); len(evs) != 1 || evs[0].endpoint != 2 {
		t.Errorf("events %+v, want one on endpoint 2", evs)
	}
	if err := srv.Emit(2, opdef.EventOperationCompletion, struct{ CompletionErrorCode uint8 }{0x7F}); !errors.Is(err, opstate.ErrUnknownError) {
		t.Errorf("Emit(unknown code) = %v, want ErrUnknownError", err)
	}
}

// TestSetBeforeWriteAcceptsAVeto: every attribute is read-only, so the
// veto is set and removed without ever running.
func TestSetBeforeWriteAcceptsAVeto(t *testing.T) {
	t.Parallel()

	srv, _ := phasedWasher(t)
	ran := false
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { ran = true; return nil })
	if err := srv.MatterWrite(context.Background(), opdef.AttrOperationalState, uint8(opstate.StatePaused)); err == nil {
		t.Error("a write of the read-only OperationalState succeeded")
	}
	if ran {
		t.Error("the veto ran for a read-only attribute")
	}
	srv.SetBeforeWrite(nil)
}

// TestAHandlerReplyOutsideTheErrorStatesIsAFailure: the reply is held to
// the cluster's error states, and the failure carries IM Failure.
func TestAHandlerReplyOutsideTheErrorStatesIsAFailure(t *testing.T) {
	t.Parallel()

	d := &device{reply: opstate.ErrorState{ID: 0x7F}}
	srv := newWasher(t, d, nil)
	_, err := srv.MatterInvoke(context.Background(), opdef.CmdStart, nil)
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusFailure {
		t.Fatalf("MatterInvoke = %v, want an IM Failure", err)
	}
	if !strings.Contains(err.Error(), opstate.ErrInvalidHandlerReply.Error()) {
		t.Errorf("error %q does not name the invalid reply", err)
	}
}

// TestHostOwnedDataVersion: a host's tracker is the one the server
// advances.
func TestHostOwnedDataVersion(t *testing.T) {
	t.Parallel()

	dv := &cluster.DataVersionTracker{}
	srv := newWasher(t, &device{}, func(cfg *opstate.Config) { cfg.DataVersion = dv })
	before := dv.Current()
	if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
		t.Fatal(err)
	}
	if dv.Current() == before || srv.MatterDataVersion() != dv.Current() {
		t.Errorf("host tracker %d (was %d), server %d", dv.Current(), before, srv.MatterDataVersion())
	}
}
