// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	"github.com/SukramJ/go-fabric/contract"
)

// TestALabelForAStandardStateIsRefused (R2-P6-par-1567): a label belongs
// to a manufacturer-specific state; one given for a standard state is
// refused rather than dropped, and an empty label of a manufacturer state
// is admitted.
func TestALabelForAStandardStateIsRefused(t *testing.T) {
	t.Parallel()
	_, err := opstate.NewServer(opstate.Config{States: []opstate.StateEntry{{ID: opstate.StateStopped, Label: "idle"}, {ID: opstate.StateError}}})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a label for Stopped: %v, want spec.ErrInvalidValue", err)
	}
	if _, err := opstate.NewServer(opstate.Config{States: []opstate.StateEntry{{ID: 0x80, Label: ""}, {ID: 0x81, Label: "drying"}, {ID: opstate.StateError}}, State: opstate.StateError}); err != nil {
		t.Errorf("manufacturer states with and without a label: %v", err)
	}
}

// TestAnErrorBesideAnotherStateIsRefused (R2-P6-fsm-1568): SetAttributes
// with an OperationalError other than NoError and an OperationalState
// other than Error is refused before anything is applied, so no
// OperationalError event announces an error the state then clears.
func TestAnErrorBesideAnotherStateIsRefused(t *testing.T) {
	t.Parallel()
	srv, err := opstate.NewServer(opstate.Config{States: []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StateError}}, State: opstate.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	rec := &countingEmitter{}
	srv.SetMatterEventEmitter(rec)
	failure := opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateUnableToStartOrResume}
	err = srv.SetAttributes(map[uint32]any{opdef.AttrOperationalError: failure, opdef.AttrOperationalState: uint8(opstate.StateStopped)})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("error with Stopped: %v, want spec.ErrInvalidValue", err)
	}
	if rec.n != 0 {
		t.Errorf("%d events emitted by the refused call", rec.n)
	}
	if v, _ := srv.MatterRead(opdef.AttrOperationalState); v != uint8(opstate.StateRunning) {
		t.Errorf("OperationalState %v after the refused call, want Running", v)
	}
	// The same error with Error is the consistent pair.
	if err := srv.SetAttributes(map[uint32]any{opdef.AttrOperationalError: failure, opdef.AttrOperationalState: uint8(opstate.StateError)}); err != nil {
		t.Errorf("error with Error: %v", err)
	}
}

type countingEmitter struct{ n int }

func (c *countingEmitter) MatterEmitEvent(uint16, uint32, uint32, any, contract.EventPriority) { c.n++ }
