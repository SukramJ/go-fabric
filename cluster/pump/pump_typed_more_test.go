// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/spec"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
)

// TestPumpSetChecksBeforeRefusing: SetAttributes checks every value
// against the attribute's type first — a value of the wrong type is
// ErrInvalidValue, an unserved attribute ErrNotServed — and only a valid
// value is refused as not held.
func TestPumpSetChecksBeforeRefusing(t *testing.T) {
	t.Parallel()
	srv := newFull(t, &device{st: pump.State{OperationMode: pump.OperationNormal}})
	if err := srv.SetAttributes(map[uint32]any{pumpdef.AttrOperationMode: "normal"}); !errors.Is(err, spec.ErrInvalidValue) || errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("a string OperationMode: %v, want ErrInvalidValue", err)
	}
	if err := srv.SetAttributes(map[uint32]any{0x7FFF: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v, want ErrNotServed", err)
	}
	if err := srv.SetAttributes(map[uint32]any{pumpdef.AttrOperationMode: uint8(pump.OperationMaximum)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("a valid OperationMode: %v, want ErrNotHeld", err)
	}
}

// TestPumpSupportedModes: the operation and control modes the feature
// selection offers (SPD, PRSCONST, AUTO and LOCAL in newFull).
func TestPumpSupportedModes(t *testing.T) {
	t.Parallel()
	srv := newFull(t, &device{})
	for m, want := range map[pump.OperationMode]bool{
		pump.OperationNormal: true, pump.OperationMinimum: true, pump.OperationMaximum: true, pump.OperationLocal: true,
	} {
		if got := srv.SupportsOperationMode(m); got != want {
			t.Errorf("SupportsOperationMode(%v) = %t, want %t", m, got, want)
		}
	}
	for m, want := range map[pump.ControlMode]bool{
		pump.ControlConstantSpeed: true, pump.ControlConstantPressure: true, pump.ControlAutomatic: true,
		pump.ControlConstantFlow: false, pump.ControlConstantTemperature: false, pump.ControlProportionalPressure: false,
	} {
		if got := srv.SupportsControlMode(m); got != want {
			t.Errorf("SupportsControlMode(%v) = %t, want %t", m, got, want)
		}
	}
}
