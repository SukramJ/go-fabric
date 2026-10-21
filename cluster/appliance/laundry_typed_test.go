// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	"github.com/SukramJ/go-fabric/contract"
)

func ptrTo[T any](v T) *T { return &v }

// TestTypedLaundryWasherControls drives the self-held washer controls
// through the typed server: Set checks the spin speed and the rinses, the
// configured lists are fixed, the cluster has no command and no event, and
// OnWrite vetoes a controller's write.
func TestTypedLaundryWasherControls(t *testing.T) {
	t.Parallel()
	srv, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
		Features:   uint32(lwc.FeatureSpin | lwc.FeatureRinse),
		SpinSpeeds: []string{"400", "800", "1200"}, SupportedRinses: []appliance.NumberOfRinses{lwc.NumberOfRinsesNormal, lwc.NumberOfRinsesExtra},
		NumberOfRinses: lwc.NumberOfRinsesNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := lwc.Wrap(srv)
	changes := 0
	defer s.OnChanged(func(lwc.State, contract.Change) { changes++ })()
	if err := s.Set(lwc.Patch{SpinSpeedCurrent: ptrTo(uint8(2)), NumberOfRinses: ptrTo(lwc.NumberOfRinsesExtra)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.SpinSpeedCurrent == nil || *st.SpinSpeedCurrent != 2 || st.NumberOfRinses != lwc.NumberOfRinsesExtra || changes != 1 {
		t.Errorf("state %+v, %d changes", st, changes)
	}
	if err := s.Set(lwc.Patch{SpinSpeedCurrent: ptrTo(uint8(3))}); err == nil {
		t.Error("a spin speed outside SpinSpeeds was set")
	}
	if err := s.Set(lwc.Patch{SpinSpeeds: &[]string{"1"}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("SpinSpeeds: %v", err)
	}
	if err := srv.Handle(0x00, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a command the cluster does not have: %v", err)
	}
	if err := spec.EmitOn(srv, 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event the cluster does not define: %v", err)
	}
	refusal := errors.New("not now")
	if err := s.OnWrite(func(context.Context, lwc.State, uint32) error { return refusal }); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), lwc.AttrSpinSpeedCurrent, uint64(0)); !errors.Is(err, refusal) {
		t.Errorf("a vetoed write: %v", err)
	}
}

// TestTypedLaundryDryerControls: Set checks the dryness level against the
// supported ones; the supported list is fixed.
func TestTypedLaundryDryerControls(t *testing.T) {
	t.Parallel()
	srv, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{ldc.DrynessLevelLow, ldc.DrynessLevelExtra}})
	if err != nil {
		t.Fatal(err)
	}
	s := ldc.Wrap(srv)
	if err := s.Set(ldc.Patch{SelectedDrynessLevel: ptrTo(ldc.DrynessLevelExtra)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.SelectedDrynessLevel == nil || *st.SelectedDrynessLevel != ldc.DrynessLevelExtra {
		t.Errorf("state %+v", st)
	}
	if err := s.Set(ldc.Patch{SelectedDrynessLevel: ptrTo(ldc.DrynessLevelNormal)}); err == nil {
		t.Error("an unsupported dryness level was set")
	}
	if err := s.Set(ldc.Patch{SupportedDrynessLevels: &[]ldc.DrynessLevelEnum{ldc.DrynessLevelLow}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("SupportedDrynessLevels: %v", err)
	}
}
