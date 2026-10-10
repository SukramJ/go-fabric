// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modebase"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	matterparity "github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// snapshotEnums reads a cluster's enum datatypes from the matter.js
// snapshot: datatype name → value name → value.
func snapshotEnums(t *testing.T, clusterID uint32) map[string]map[string]uint64 {
	t.Helper()
	var s struct {
		Clusters []struct {
			ID        uint32 `json:"id"`
			Datatypes []struct {
				Name   string `json:"name"`
				Fields []struct {
					ID   uint64 `json:"id"`
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"datatypes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(matterparity.SchemaJSON(), &s); err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Clusters {
		if c.ID != clusterID {
			continue
		}
		out := map[string]map[string]uint64{}
		for _, d := range c.Datatypes {
			out[d.Name] = map[string]uint64{}
			for _, f := range d.Fields {
				out[d.Name][f.Name] = f.ID
			}
		}
		return out
	}
	t.Fatalf("no cluster 0x%04X in the snapshot", clusterID)
	return nil
}

// TestParityMatterJS_ModeBaseTrancheTags pins the tag constants of the six
// derivations added with OvenMode against each derivation's ModeTag
// datatype in the snapshot.
func TestParityMatterJS_ModeBaseTrancheTags(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cluster uint32
		tags    map[string]uint16
	}{
		{modebase.ClusterIDOvenMode, map[string]uint16{
			"Auto": modebase.TagAuto, "Day": modebase.TagDay,
			"Bake": modebase.OvenTagBake, "Convection": modebase.OvenTagConvection, "Grill": modebase.OvenTagGrill,
			"Roast": modebase.OvenTagRoast, "Clean": modebase.OvenTagClean, "ConvectionBake": modebase.OvenTagConvectionBake,
			"ConvectionRoast": modebase.OvenTagConvectionRoast, "Warming": modebase.OvenTagWarming,
			"Proofing": modebase.OvenTagProofing, "Steam": modebase.OvenTagSteam, "AirFry": modebase.OvenTagAirFry,
			"AirSousVide": modebase.OvenTagAirSousVide, "FrozenFood": modebase.OvenTagFrozenFood,
		}},
		{modebase.ClusterIDRefrigeratorAndTemperatureControlledCabinetMode, map[string]uint16{
			"Auto": modebase.TagAuto, "RapidCool": modebase.RefrigeratorTagRapidCool, "RapidFreeze": modebase.RefrigeratorTagRapidFreeze,
		}},
		{modebase.ClusterIDMicrowaveOvenMode, map[string]uint16{
			"Normal": modebase.MicrowaveTagNormal, "Defrost": modebase.MicrowaveTagDefrost,
		}},
		{modebase.ClusterIDEnergyEvseMode, map[string]uint16{
			"Manual": modebase.EvseTagManual, "TimeOfUse": modebase.EvseTagTimeOfUse,
			"SolarCharging": modebase.EvseTagSolarCharging, "V2X": modebase.EvseTagV2X,
		}},
		{modebase.ClusterIDWaterHeaterMode, map[string]uint16{
			"Off": modebase.WaterHeaterTagOff, "Manual": modebase.WaterHeaterTagManual, "Timed": modebase.WaterHeaterTagTimed,
		}},
		{modebase.ClusterIDDeviceEnergyManagementMode, map[string]uint16{
			"NoOptimization": modebase.DemTagNoOptimization, "DeviceOptimization": modebase.DemTagDeviceOptimization,
			"LocalOptimization": modebase.DemTagLocalOptimization, "GridOptimization": modebase.DemTagGridOptimization,
		}},
	} {
		js := snapshotEnums(t, c.cluster)["ModeTag"]
		for name, v := range c.tags {
			if want, ok := js[name]; !ok || want != uint64(v) {
				t.Errorf("0x%04X ModeTag %s = %d, matter.js %d (present %v)", c.cluster, name, v, want, ok)
			}
		}
	}
}

// TestParityMatterJS_ModeChangeStatusAgainstSnapshot holds, for every
// derivation that accepts ChangeToMode, the statuses a device may answer
// to the derivation's ModeChangeStatus datatype in the snapshot: every
// value below 0x80 the snapshot lists passes except UnsupportedMode (the
// server's own answer), every other one below 0x80 fails the invoke.
func TestParityMatterJS_ModeChangeStatusAgainstSnapshot(t *testing.T) {
	t.Parallel()
	for _, d := range derivations(t) {
		if len(d.srv.MatterAcceptedCommands()) == 0 {
			continue
		}
		js := snapshotEnums(t, d.srv.MatterClusterID())["ModeChangeStatus"]
		listed := map[uint64]bool{}
		for _, v := range js {
			listed[v] = true
		}
		if js["UnsupportedMode"] != uint64(modebase.StatusUnsupportedMode) || js["Success"] != uint64(modebase.StatusSuccess) {
			t.Fatalf("%s ModeChangeStatus %v", d.name, js)
		}
		for st := range uint8(0x80) {
			srv := rebuild(t, d.name, &changer{status: modebase.Status(st)})
			resp, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, changeRequestTo(srv))
			want := listed[uint64(st)] && modebase.Status(st) != modebase.StatusUnsupportedMode
			if (err == nil) != want {
				t.Errorf("%s host status 0x%02X: %+v, %v (listed %v)", d.name, st, resp, err, listed[uint64(st)])
			}
		}
	}
}

// TestParityMatterJS_ModeBaseTrancheDeviceTypes pins the device types the
// new derivations serve, from the generated device type tables.
func TestParityMatterJS_ModeBaseTrancheDeviceTypes(t *testing.T) {
	t.Parallel()
	const refrigerator, cabinet, microwave, evse, waterHeater, dem = 0x0070, 0x0071, 0x0079, 0x050C, 0x050F, 0x050D
	for _, c := range []struct{ dt, cluster uint32 }{
		{microwave, modebase.ClusterIDMicrowaveOvenMode},
		{evse, modebase.ClusterIDEnergyEvseMode},
		{waterHeater, modebase.ClusterIDWaterHeaterMode},
	} {
		if !schema.DeviceTypeRequiresServerCluster(c.dt, c.cluster) {
			t.Errorf("device type 0x%04X does not mandate 0x%04X", c.dt, c.cluster)
		}
	}
	for _, c := range []struct{ dt, cluster uint32 }{
		{refrigerator, modebase.ClusterIDRefrigeratorAndTemperatureControlledCabinetMode},
		{cabinet, modebase.ClusterIDRefrigeratorAndTemperatureControlledCabinetMode},
		{cabinet, modebase.ClusterIDOvenMode},
		{dem, modebase.ClusterIDDeviceEnergyManagementMode},
	} {
		if ok, _ := schema.DeviceTypeAllowsServerCluster(c.dt, c.cluster); !ok {
			t.Errorf("device type 0x%04X does not allow 0x%04X", c.dt, c.cluster)
		}
	}
}

// rebuild builds the named derivation again with c as its changer,
// current mode the first fixture mode.
func rebuild(t *testing.T, name string, c *changer) *modebase.Server {
	t.Helper()
	fixtures := map[string]struct {
		build func(modebase.Config) (*modebase.Server, error)
		modes []modebase.ModeOption
	}{
		"LaundryWasherMode": {modebase.NewLaundryWasherMode, laundryModes},
		"RvcRunMode":        {modebase.NewRvcRunMode, runModes},
		"RvcCleanMode":      {modebase.NewRvcCleanMode, cleanModes},
		"DishwasherMode":    {modebase.NewDishwasherMode, dishModes},
		"OvenMode":          {modebase.NewOvenMode, ovenModes},
		"RefrigeratorAndTemperatureControlledCabinetMode": {modebase.NewRefrigeratorAndTemperatureControlledCabinetMode, fridgeModes},
		"EnergyEvseMode":             {modebase.NewEnergyEvseMode, evseModes},
		"WaterHeaterMode":            {modebase.NewWaterHeaterMode, waterHeaterModes},
		"DeviceEnergyManagementMode": {modebase.NewDeviceEnergyManagementMode, demModes},
	}
	f, ok := fixtures[name]
	if !ok {
		t.Fatalf("no fixture for %s", name)
	}
	srv, err := f.build(modebase.Config{Changer: c, SupportedModes: f.modes, CurrentMode: f.modes[0].Mode})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// changeRequestTo asks for the second supported mode, never the current one.
func changeRequestTo(srv *modebase.Server) clusterwire.ChangeToModeRequest {
	v, _ := srv.MatterRead(modebase.AttrSupportedModes)
	return clusterwire.ChangeToModeRequest{NewMode: v.([]clusterwire.ModeOptionStruct)[1].Mode}
}
