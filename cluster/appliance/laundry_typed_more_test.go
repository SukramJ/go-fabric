// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
)

// TestTypedLaundrySetRefusals: an attribute the server does not serve is
// refused before anything is stored, and the dryer's SupportedDrynessLevels
// is the configuration's.
func TestTypedLaundrySetRefusals(t *testing.T) {
	t.Parallel()
	washer, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
		Features: uint32(lwc.FeatureSpin), SpinSpeeds: []string{"400", "800"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := washer.SetAttributes(map[uint32]any{lwc.AttrNumberOfRinses: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("NumberOfRinses without RINSE: %v", err)
	}

	dryer, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{ldc.DrynessLevelLow, ldc.DrynessLevelExtra}})
	if err != nil {
		t.Fatal(err)
	}
	if err := dryer.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	s := ldc.Wrap(dryer)
	if err := s.Set(ldc.Patch{SupportedDrynessLevels: ptrTo([]ldc.DrynessLevelEnum{ldc.DrynessLevelLow})}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("SupportedDrynessLevels: %v", err)
	}
	if err := s.Set(ldc.Patch{SupportedDrynessLevels: ptrTo([]ldc.DrynessLevelEnum{ldc.DrynessLevelLow, ldc.DrynessLevelExtra})}); err != nil {
		t.Errorf("the configured levels themselves: %v", err)
	}
}

// TestTypedWasherConfiguredLists: SpinSpeeds and SupportedRinses are the
// configuration's — their own value is no change, any other is refused.
func TestTypedWasherConfiguredLists(t *testing.T) {
	t.Parallel()
	washer, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
		Features:   uint32(lwc.FeatureSpin | lwc.FeatureRinse),
		SpinSpeeds: []string{"400", "800"}, SupportedRinses: []appliance.NumberOfRinses{lwc.NumberOfRinsesNormal},
		NumberOfRinses: lwc.NumberOfRinsesNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := lwc.Wrap(washer)
	if err := s.Set(lwc.Patch{SpinSpeeds: ptrTo([]string{"400", "800"}), SupportedRinses: ptrTo([]lwc.NumberOfRinsesEnum{lwc.NumberOfRinsesNormal})}); err != nil {
		t.Errorf("the configured lists themselves: %v", err)
	}
	if err := s.Set(lwc.Patch{SpinSpeeds: ptrTo([]string{"1200"})}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("other SpinSpeeds: %v", err)
	}
	if err := s.Set(lwc.Patch{SupportedRinses: ptrTo([]lwc.NumberOfRinsesEnum{lwc.NumberOfRinsesExtra})}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("other SupportedRinses: %v", err)
	}
	if err := s.Set(lwc.Patch{NumberOfRinses: ptrTo(lwc.NumberOfRinsesExtra)}); err == nil {
		t.Error("a NumberOfRinses outside SupportedRinses was set")
	}
	// The stored list form compares as the configured list too.
	stored, _ := washer.MatterRead(lwc.AttrSupportedRinses)
	if err := washer.SetAttributes(map[uint32]any{lwc.AttrSupportedRinses: stored}); err != nil {
		t.Errorf("the stored SupportedRinses: %v", err)
	}
}
