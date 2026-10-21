// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/spec"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/im"
)

// TestMicrowaveSetAttributesHoldsEachValueToItsSetterRule: CookTime,
// PowerSetting and SelectedWattIndex are the device's, each checked as its
// setter checks it; the configuration's attributes are ErrNotHeld; nothing
// is stored when any value of the call is refused.
func TestMicrowaveSetAttributesHoldsEachValueToItsSetterRule(t *testing.T) {
	t.Parallel()

	number := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerAsNumber|appliance.MicrowaveFeaturePowerNumberLimits)
	watts := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerInWatts)

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		if err := number.SetAttributes(nil); err != nil {
			t.Fatalf("empty call = %v", err)
		}
		if err := number.SetAttributes(map[uint32]any{mwo.AttrCookTime: uint32(300), mwo.AttrPowerSetting: uint8(40)}); err != nil {
			t.Fatalf("CookTime and PowerSetting = %v", err)
		}
		if got := number.CookTime(); got != 300 {
			t.Errorf("CookTime = %d, want 300", got)
		}
		if err := watts.SetAttributes(map[uint32]any{mwo.AttrSelectedWattIndex: uint8(1)}); err != nil {
			t.Fatalf("SelectedWattIndex = %v", err)
		}
	})

	tests := []struct {
		name   string
		srv    *appliance.MicrowaveServer
		values map[uint32]any
		status im.StatusCode
		is     error
	}{
		{"CookTime above MaxCookTime", number, map[uint32]any{mwo.AttrCookTime: uint32(601)}, im.StatusConstraintError, nil},
		{"PowerSetting above the limit", number, map[uint32]any{mwo.AttrPowerSetting: uint8(100)}, im.StatusConstraintError, nil},
		{"PowerSetting off the step", number, map[uint32]any{mwo.AttrPowerSetting: uint8(30)}, im.StatusConstraintError, nil},
		{"SelectedWattIndex above SupportedWatts", watts, map[uint32]any{mwo.AttrSelectedWattIndex: uint8(3)}, im.StatusConstraintError, nil},
		{"MaxCookTime is the configuration's", number, map[uint32]any{mwo.AttrMaxCookTime: uint32(900)}, 0, spec.ErrNotHeld},
		{"SupportedWatts is the configuration's", watts, map[uint32]any{mwo.AttrSupportedWatts: []uint16{300}}, 0, spec.ErrNotHeld},
		{"an unserved attribute", number, map[uint32]any{0x7F: uint8(1)}, 0, spec.ErrNotServed},
		{"SelectedWattIndex without WATTS", number, map[uint32]any{mwo.AttrSelectedWattIndex: uint8(0)}, 0, spec.ErrNotServed},
		{"a value of the wrong type", number, map[uint32]any{mwo.AttrCookTime: "soon"}, 0, spec.ErrInvalidValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.srv.SetAttributes(tt.values)
			if err == nil {
				t.Fatal("SetAttributes accepted the value")
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("err = %v, want %v", err, tt.is)
			}
			if tt.is == nil && status(t, err) != tt.status {
				t.Errorf("status = %v, want %v", status(t, err), tt.status)
			}
		})
	}

	t.Run("all or nothing", func(t *testing.T) {
		t.Parallel()
		srv := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerAsNumber|appliance.MicrowaveFeaturePowerNumberLimits)
		before := srv.CookTime()
		// Ids are checked in order: CookTime (valid) then PowerSetting (not).
		err := srv.SetAttributes(map[uint32]any{mwo.AttrCookTime: uint32(250), mwo.AttrPowerSetting: uint8(30)})
		if err == nil {
			t.Fatal("a refused PowerSetting was accepted")
		}
		if got := srv.CookTime(); got != before {
			t.Errorf("CookTime = %d after a refused call, want %d", got, before)
		}
	})
}
