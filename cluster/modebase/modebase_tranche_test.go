// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	"github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	"github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	"github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	"github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	"github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	rtcc "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	"github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

var ovenModes = []modebase.ModeOption{
	{Label: "Bake", Mode: 0, Tags: tags(modebase.OvenTagBake)},
	{Label: "Convection", Mode: 1, Tags: tags(modebase.OvenTagConvection)},
}

var fridgeModes = []modebase.ModeOption{
	{Label: "Auto", Mode: 0, Tags: tags(modebase.TagAuto)},
	{Label: "Rapid Cool", Mode: 1, Tags: tags(modebase.RefrigeratorTagRapidCool)},
}

var microwaveModes = []modebase.ModeOption{
	{Label: "Normal", Mode: 0, Tags: tags(modebase.MicrowaveTagNormal)},
	{Label: "Defrost", Mode: 1, Tags: tags(modebase.MicrowaveTagDefrost)},
}

var evseModes = []modebase.ModeOption{
	{Label: "Manual", Mode: 0, Tags: tags(modebase.EvseTagManual)},
	{Label: "Time of use", Mode: 1, Tags: tags(modebase.EvseTagTimeOfUse)},
}

var waterHeaterModes = []modebase.ModeOption{
	{Label: "Off", Mode: 0, Tags: tags(modebase.WaterHeaterTagOff)},
	{Label: "Manual", Mode: 1, Tags: tags(modebase.WaterHeaterTagManual)},
	{Label: "Timed", Mode: 2, Tags: tags(modebase.WaterHeaterTagTimed)},
}

var demModes = []modebase.ModeOption{
	{Label: "No optimization", Mode: 0, Tags: tags(modebase.DemTagNoOptimization)},
	{Label: "Local", Mode: 1, Tags: tags(modebase.DemTagLocalOptimization)},
	{Label: "Grid", Mode: 2, Tags: tags(modebase.DemTagGridOptimization)},
	{Label: "Device", Mode: 3, Tags: tags(modebase.DemTagDeviceOptimization, modebase.DemTagLocalOptimization)},
}

func mode(label string, value uint8, values ...uint16) modebase.ModeOption {
	return modebase.ModeOption{Label: label, Mode: value, Tags: tags(values...)}
}

// TestTrancheConstructionChecks walks each new derivation's
// #assertSupportedModes: the fixtures pass, and each case breaks one rule.
func TestTrancheConstructionChecks(t *testing.T) {
	t.Parallel()
	c := &changer{}
	cases := []struct {
		name  string
		build func(modebase.Config) (*modebase.Server, error)
		modes []modebase.ModeOption
		want  error
	}{
		// The fixtures themselves.
		{"oven", modebase.NewOvenMode, ovenModes, nil},
		{"fridge", modebase.NewRefrigeratorAndTemperatureControlledCabinetMode, fridgeModes, nil},
		{"microwave", modebase.NewMicrowaveOvenMode, microwaveModes, nil},
		{"evse", modebase.NewEnergyEvseMode, evseModes, nil},
		{"water heater", modebase.NewWaterHeaterMode, waterHeaterModes, nil},
		{"dem", modebase.NewDeviceEnergyManagementMode, demModes, nil},
		// OvenMode: at least one Bake mode.
		{"oven without Bake", modebase.NewOvenMode, []modebase.ModeOption{mode("G", 0, modebase.OvenTagGrill), mode("R", 1, modebase.OvenTagRoast)}, modebase.ErrRequiredTag},
		// RefrigeratorAndTemperatureControlledCabinetMode: at least one Auto mode.
		{"fridge without Auto", modebase.NewRefrigeratorAndTemperatureControlledCabinetMode, []modebase.ModeOption{mode("C", 0, modebase.RefrigeratorTagRapidCool), mode("F", 1, modebase.RefrigeratorTagRapidFreeze)}, modebase.ErrRequiredTag},
		// MicrowaveOvenMode: exactly one Normal, never with Defrost.
		{"microwave without Normal", modebase.NewMicrowaveOvenMode, []modebase.ModeOption{mode("D", 0, modebase.MicrowaveTagDefrost), mode("Q", 1, modebase.TagQuick)}, modebase.ErrRequiredTag},
		{"microwave with two Normal", modebase.NewMicrowaveOvenMode, []modebase.ModeOption{mode("N", 0, modebase.MicrowaveTagNormal), mode("NQ", 1, modebase.MicrowaveTagNormal, modebase.TagQuick)}, modebase.ErrRequiredTag},
		{"microwave Normal with Defrost", modebase.NewMicrowaveOvenMode, []modebase.ModeOption{mode("ND", 0, modebase.MicrowaveTagNormal, modebase.MicrowaveTagDefrost), mode("Q", 1, modebase.TagQuick)}, modebase.ErrTagCombination},
		// EnergyEvseMode: a Manual mode without TimeOfUse and SolarCharging.
		{"evse without Manual", modebase.NewEnergyEvseMode, []modebase.ModeOption{mode("T", 0, modebase.EvseTagTimeOfUse), mode("S", 1, modebase.EvseTagSolarCharging)}, modebase.ErrRequiredTag},
		{"evse Manual only with TimeOfUse", modebase.NewEnergyEvseMode, []modebase.ModeOption{mode("MT", 0, modebase.EvseTagManual, modebase.EvseTagTimeOfUse), mode("S", 1, modebase.EvseTagSolarCharging)}, modebase.ErrRequiredTag},
		{"evse Manual only with SolarCharging", modebase.NewEnergyEvseMode, []modebase.ModeOption{mode("MS", 0, modebase.EvseTagManual, modebase.EvseTagSolarCharging), mode("T", 1, modebase.EvseTagTimeOfUse)}, modebase.ErrRequiredTag},
		{"evse Manual with V2X", modebase.NewEnergyEvseMode, []modebase.ModeOption{mode("MV", 0, modebase.EvseTagManual, modebase.EvseTagV2X), mode("T", 1, modebase.EvseTagTimeOfUse)}, nil},
		// WaterHeaterMode: Manual and Off both present; Off, Manual and
		// Timed only in single-tag modes.
		{"water heater without Off", modebase.NewWaterHeaterMode, []modebase.ModeOption{mode("M", 0, modebase.WaterHeaterTagManual), mode("T", 1, modebase.WaterHeaterTagTimed)}, modebase.ErrRequiredTag},
		{"water heater without Manual", modebase.NewWaterHeaterMode, []modebase.ModeOption{mode("O", 0, modebase.WaterHeaterTagOff), mode("T", 1, modebase.WaterHeaterTagTimed)}, modebase.ErrRequiredTag},
		{"water heater Timed with another tag", modebase.NewWaterHeaterMode, append(waterHeaterModes[:2:2], mode("TQ", 2, modebase.WaterHeaterTagTimed, modebase.TagQuick)), modebase.ErrTagCombination},
		{"water heater Manual with a manufacturer tag", modebase.NewWaterHeaterMode, []modebase.ModeOption{waterHeaterModes[0], {Label: "M", Mode: 1, Tags: []modebase.ModeTag{{Value: modebase.WaterHeaterTagManual}, mfg(0xFFF1, 0x8000)}}}, modebase.ErrTagCombination},
		{"water heater Off and Manual together", modebase.NewWaterHeaterMode, append(waterHeaterModes[:2:2], mode("OM", 2, modebase.WaterHeaterTagOff, modebase.WaterHeaterTagManual)), modebase.ErrTagCombination},
		// DeviceEnergyManagementMode: NoOptimization, LocalOptimization and
		// GridOptimization present; NoOptimization never with them.
		{"dem without Grid", modebase.NewDeviceEnergyManagementMode, demModes[:2], modebase.ErrRequiredTag},
		{"dem without NoOptimization", modebase.NewDeviceEnergyManagementMode, demModes[1:], modebase.ErrRequiredTag},
		{"dem without Local", modebase.NewDeviceEnergyManagementMode, []modebase.ModeOption{demModes[0], demModes[2]}, modebase.ErrRequiredTag},
		{"dem NoOptimization with Device", modebase.NewDeviceEnergyManagementMode, append(demModes[:3:3], mode("ND", 3, modebase.DemTagNoOptimization, modebase.DemTagDeviceOptimization)), modebase.ErrTagCombination},
		{"dem NoOptimization with Grid", modebase.NewDeviceEnergyManagementMode, append(demModes[:3:3], mode("NG", 3, modebase.DemTagNoOptimization, modebase.DemTagGridOptimization)), modebase.ErrTagCombination},
	}
	for _, tc := range cases {
		if _, err := tc.build(modebase.Config{Changer: c, SupportedModes: tc.modes, CurrentMode: tc.modes[0].Mode}); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// The ModeBase rules and ModeUtils.assertMode hold for the new
	// derivations too.
	if _, err := modebase.NewOvenMode(modebase.Config{Changer: c, SupportedModes: ovenModes[:1]}); !errors.Is(err, modebase.ErrModeCount) {
		t.Errorf("one oven mode: %v", err)
	}
	if _, err := modebase.NewEnergyEvseMode(modebase.Config{Changer: c, SupportedModes: evseModes, CurrentMode: 9}); !errors.Is(err, modebase.ErrUnsupportedMode) {
		t.Errorf("evse current mode 9: %v", err)
	}
	// A Changer is required where ChangeToMode is accepted, and only there.
	if _, err := modebase.NewOvenMode(modebase.Config{SupportedModes: ovenModes}); !errors.Is(err, modebase.ErrNoChanger) {
		t.Errorf("oven without changer: %v", err)
	}
	if _, err := modebase.NewMicrowaveOvenMode(modebase.Config{SupportedModes: microwaveModes}); err != nil {
		t.Errorf("microwave without changer: %v", err)
	}
	if _, err := modebase.NewOvenMode(modebase.Config{Changer: c, SupportedModes: ovenModes, Features: modebase.FeatureDirectModeChange}); !errors.Is(err, modebase.ErrUnknownFeature) {
		t.Errorf("DIRECTMODECH on OvenMode: %v", err)
	}
}

// TestChangeToModeAcceptsGeneratedRequests: the bridge hands the new
// derivations the generated definitions' ChangeToModeRequest and the old
// four cluster/wire's; MatterInvoke takes both.
func TestChangeToModeAcceptsGeneratedRequests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		build func(modebase.Config) (*modebase.Server, error)
		modes []modebase.ModeOption
		req   any
	}{
		{modebase.NewOvenMode, ovenModes, ovenmode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewRefrigeratorAndTemperatureControlledCabinetMode, fridgeModes, rtcc.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewEnergyEvseMode, evseModes, energyevsemode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewWaterHeaterMode, waterHeaterModes, waterheatermode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewDeviceEnergyManagementMode, demModes, deviceenergymanagementmode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewLaundryWasherMode, laundryModes, laundrywashermode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewDishwasherMode, dishModes, dishwashermode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewRvcRunMode, runModes, rvcrunmode.ChangeToModeRequest{NewMode: 1}},
		{modebase.NewRvcCleanMode, cleanModes, rvccleanmode.ChangeToModeRequest{NewMode: 2}},
		{modebase.NewOvenMode, ovenModes, clusterwire.ChangeToModeRequest{NewMode: 1}},
	} {
		c := &changer{}
		srv, err := tc.build(modebase.Config{Changer: c, SupportedModes: tc.modes, CurrentMode: tc.modes[0].Mode})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, tc.req)
		if err != nil || resp != (clusterwire.ChangeToModeResponse{Status: uint8(modebase.StatusSuccess)}) || current(srv) == tc.modes[0].Mode || len(c.asked) != 1 {
			t.Errorf("0x%04X %T: %+v, %v, current %d", srv.MatterClusterID(), tc.req, resp, err, current(srv))
		}
		// An unsupported mode through a generated request: UnsupportedMode.
		resp, err = srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, ovenmode.ChangeToModeRequest{NewMode: 200})
		if r, ok := resp.(clusterwire.ChangeToModeResponse); err != nil || !ok || r.Status != uint8(modebase.StatusUnsupportedMode) {
			t.Errorf("0x%04X unsupported mode: %+v, %v", srv.MatterClusterID(), resp, err)
		}
	}
}

// TestMicrowaveOvenModeAcceptsNoCommand: the definition's ChangeToMode is
// "X", so the server answers UNSUPPORTED_COMMAND whatever the payload.
func TestMicrowaveOvenModeAcceptsNoCommand(t *testing.T) {
	t.Parallel()
	srv, err := modebase.NewMicrowaveOvenMode(modebase.Config{SupportedModes: microwaveModes})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.MatterAcceptedCommands()) != 0 || len(srv.MatterGeneratedCommands()) != 0 {
		t.Errorf("command lists %v / %v", srv.MatterAcceptedCommands(), srv.MatterGeneratedCommands())
	}
	var sce im.StatusCodeError
	for _, req := range []any{microwaveovenmode.ChangeToModeRequest{NewMode: 1}, clusterwire.ChangeToModeRequest{NewMode: 1}} {
		if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, req); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
			t.Errorf("%T: %v", req, err)
		}
	}
	// The device still moves CurrentMode out of band.
	if err := srv.SetCurrentMode(1); err != nil || current(srv) != 1 {
		t.Errorf("SetCurrentMode: %v", err)
	}
}
