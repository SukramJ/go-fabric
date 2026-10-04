// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// modeBase is ModeBase itself (mode-base.element.ts:19-56). It has no
// cluster id, so the snapshot does not carry it; each derivation's entry
// carries only what it overrides, and the base's elements are inherited
// from here.
var modeBase = struct {
	attributes map[string]struct {
		id          uint32
		conformance string
		access      string
	}
	commands map[string]struct {
		id          uint32
		conformance string
	}
}{
	attributes: map[string]struct {
		id          uint32
		conformance string
		access      string
	}{
		"SupportedModes": {0x0000, "M", "R V"},
		"CurrentMode":    {0x0001, "M", "R V"},
		"StartUpMode":    {0x0002, "O", "RW VO"},
		"OnMode":         {0x0003, "DEPONOFF", "RW VO"},
	},
	commands: map[string]struct {
		id          uint32
		conformance string
	}{
		"ChangeToMode":         {0x00, "M"},
		"ChangeToModeResponse": {0x01, "M"},
	},
}

// derivations are the four served clusters with a server built for each.
func derivations(t *testing.T) []struct {
	name string
	srv  *modebase.Server
} {
	t.Helper()
	build := func(f func(modebase.Config) (*modebase.Server, error), modes []modebase.ModeOption) *modebase.Server {
		srv, err := f(modebase.Config{Changer: &changer{}, SupportedModes: modes, CurrentMode: modes[0].Mode})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	return []struct {
		name string
		srv  *modebase.Server
	}{
		{"LaundryWasherMode", build(modebase.NewLaundryWasherMode, laundryModes)},
		{"RvcRunMode", build(modebase.NewRvcRunMode, runModes)},
		{"RvcCleanMode", build(modebase.NewRvcCleanMode, cleanModes)},
		{"DishwasherMode", build(modebase.NewDishwasherMode, dishModes)},
	}
}

// TestParityMatterJS_ModeBaseDerivations checks each derivation's
// revision, name, attribute and command ids, and the attribute list
// against the derivation's conformance (the base's where the derivation
// does not override it): StartUpMode and OnMode are "X" and not served.
func TestParityMatterJS_ModeBaseDerivations(t *testing.T) {
	t.Parallel()
	for _, d := range derivations(t) {
		js := paritytest.ClusterSnapshot(t, d.srv.MatterClusterID())
		if js.Name != d.name || js.Revision != d.srv.Revision() {
			t.Errorf("0x%04X: matter.js %s rev %d, server rev %d", d.srv.MatterClusterID(), js.Name, js.Revision, d.srv.Revision())
		}
		features := map[string]bool{}
		for _, a := range js.Attributes {
			if a.ID >= 0xFFF0 {
				continue
			}
			base, ok := modeBase.attributes[a.Name]
			if !ok || base.id != a.ID {
				t.Errorf("%s attribute %s (0x%04X) is not ModeBase's", d.name, a.Name, a.ID)
				continue
			}
			conformance := a.Conformance
			if conformance == "" {
				conformance = base.conformance
			}
			required, allowed := paritytest.Conformance(conformance, features)
			has := slices.Contains(d.srv.MatterAttributes(), a.ID)
			if required && !has || has && !allowed {
				t.Errorf("%s %s (%q) served=%v", d.name, a.Name, conformance, has)
			}
			if a.Access != "" && a.Access != base.access {
				t.Errorf("%s %s overrides access %q", d.name, a.Name, a.Access)
			}
		}
		for _, cmd := range js.Commands {
			base, ok := modeBase.commands[cmd.Name]
			if !ok || base.id != cmd.ID || (cmd.Conformance != "" && cmd.Conformance != base.conformance) {
				t.Errorf("%s command %+v differs from ModeBase", d.name, cmd)
			}
		}
		if !slices.Equal(d.srv.MatterAcceptedCommands(), []uint32{modeBase.commands["ChangeToMode"].id}) ||
			!slices.Equal(d.srv.MatterGeneratedCommands(), []uint32{modeBase.commands["ChangeToModeResponse"].id}) {
			t.Errorf("%s command lists", d.name)
		}
		if len(js.Events) != 0 || len(d.srv.MatterEvents()) != 0 {
			t.Errorf("%s events %v / %v", d.name, js.Events, d.srv.MatterEvents())
		}
	}
}

// TestParityMatterJS_ModeBaseFeatures pins the FeatureMap: DEPONOFF is
// "X" everywhere and never advertised; DIRECTMODECH is RvcRunMode's and
// RvcCleanMode's, at the bit matter.js gives it.
func TestParityMatterJS_ModeBaseFeatures(t *testing.T) {
	t.Parallel()
	for _, d := range derivations(t) {
		js := paritytest.ClusterSnapshot(t, d.srv.MatterClusterID())
		for _, f := range js.Features {
			switch f.Name {
			case "DEPONOFF":
				if f.Conformance != "X" {
					t.Errorf("%s DEPONOFF conformance %q", d.name, f.Conformance)
				}
			case "DIRECTMODECH":
				if f.Conformance != "O" || uint32(modebase.FeatureDirectModeChange) != 1<<f.Bit {
					t.Errorf("%s DIRECTMODECH %+v, constant 0x%X", d.name, f, uint32(modebase.FeatureDirectModeChange))
				}
				build := map[string]func(modebase.Config) (*modebase.Server, error){
					"RvcRunMode": modebase.NewRvcRunMode, "RvcCleanMode": modebase.NewRvcCleanMode,
				}[d.name]
				modes := map[string][]modebase.ModeOption{"RvcRunMode": runModes, "RvcCleanMode": cleanModes}[d.name]
				if build == nil {
					t.Errorf("%s defines DIRECTMODECH; the server refuses it", d.name)
					continue
				}
				if _, err := build(modebase.Config{Changer: &changer{}, SupportedModes: modes, CurrentMode: modes[0].Mode, Features: modebase.FeatureDirectModeChange}); err != nil {
					t.Errorf("%s with DIRECTMODECH: %v", d.name, err)
				}
			default:
				t.Errorf("%s feature %s is not modelled", d.name, f.Name)
			}
		}
	}
}

// TestParityMatterJS_ModeBaseDeviceTypes pins the device types the
// derivations serve: RoboticVacuumCleaner mandates RvcRunMode and allows
// RvcCleanMode; LaundryWasher and LaundryDryer allow LaundryWasherMode,
// Dishwasher DishwasherMode.
func TestParityMatterJS_ModeBaseDeviceTypes(t *testing.T) {
	t.Parallel()
	const rvc, washer, dishwasher, dryer = 0x0074, 0x0073, 0x0075, 0x007C
	if !schema.DeviceTypeRequiresServerCluster(rvc, modebase.ClusterIDRvcRunMode) {
		t.Error("RoboticVacuumCleaner does not mandate RvcRunMode")
	}
	for _, c := range []struct{ dt, cluster uint32 }{
		{rvc, modebase.ClusterIDRvcCleanMode},
		{washer, modebase.ClusterIDLaundryWasherMode},
		{dryer, modebase.ClusterIDLaundryWasherMode},
		{dishwasher, modebase.ClusterIDDishwasherMode},
	} {
		if ok, _ := schema.DeviceTypeAllowsServerCluster(c.dt, c.cluster); !ok {
			t.Errorf("device type 0x%04X does not allow 0x%04X", c.dt, c.cluster)
		}
	}
}

// TestParityMatterJS_ModeBaseEnumValues pins the tag and status values
// against mode-base.element.ts and the derivations' datatypes.
func TestParityMatterJS_ModeBaseEnumValues(t *testing.T) {
	t.Parallel()
	tagValues := []uint16{
		modebase.TagAuto, modebase.TagQuick, modebase.TagQuiet, modebase.TagLowNoise, modebase.TagLowEnergy,
		modebase.TagVacation, modebase.TagMin, modebase.TagMax, modebase.TagNight, modebase.TagDay,
		modebase.LaundryTagNormal, modebase.LaundryTagDelicate, modebase.LaundryTagHeavy, modebase.LaundryTagWhites,
		modebase.DishwasherTagNormal, modebase.DishwasherTagHeavy, modebase.DishwasherTagLight,
		modebase.RvcRunTagIdle, modebase.RvcRunTagCleaning, modebase.RvcRunTagMapping,
		modebase.RvcCleanTagDeepClean, modebase.RvcCleanTagVacuum, modebase.RvcCleanTagMop, modebase.RvcCleanTagVacuumThenMop,
	}
	wantTags := []uint16{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0x4000, 0x4001, 0x4002, 0x4003, 0x4000, 0x4001, 0x4002,
		0x4000, 0x4001, 0x4002, 0x4000, 0x4001, 0x4002, 0x4003,
	}
	if !slices.Equal(tagValues, wantTags) {
		t.Errorf("ModeTag values %v, want %v", tagValues, wantTags)
	}
	statuses := []modebase.Status{
		modebase.StatusSuccess, modebase.StatusUnsupportedMode, modebase.StatusGenericFailure, modebase.StatusInvalidInMode,
		modebase.StatusCleaningInProgress, modebase.StatusStuck, modebase.StatusDustBinMissing, modebase.StatusDustBinFull,
		modebase.StatusWaterTankEmpty, modebase.StatusWaterTankMissing, modebase.StatusWaterTankLidOpen,
		modebase.StatusMopCleaningPadMissing, modebase.StatusBatteryLow,
	}
	if want := []modebase.Status{0, 1, 2, 3, 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48}; !slices.Equal(statuses, want) {
		t.Errorf("ModeChangeStatus values %v, want %v", statuses, want)
	}
}
