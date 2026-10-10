// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_ApplianceServers builds every feature selection and
// optional element of the three clusters and holds each server against
// the matter.js conformance of its definition (spectest.CheckServer).
func TestParityMatterJS_ApplianceServers(t *testing.T) {
	t.Parallel()
	rating := uint16(1000)
	for _, f := range []uint32{
		appliance.MicrowaveFeaturePowerAsNumber,
		appliance.MicrowaveFeaturePowerAsNumber | appliance.MicrowaveFeaturePowerNumberLimits,
		appliance.MicrowaveFeaturePowerInWatts,
	} {
		for _, more := range []bool{false, true} {
			for _, wr := range []*uint16{nil, &rating} {
				srv, err := appliance.NewMicrowaveOvenControl(appliance.MicrowaveConfig{
					Features: f, Oven: &oven{}, AddMoreTime: more, WattRating: wr,
					MaxCookTime: 3600, PowerSetting: 100, MinPower: 10, MaxPower: 100, PowerStep: 10,
					SupportedWatts: []uint16{600, 800}, SelectedWattIndex: 1,
				})
				if err != nil {
					t.Fatalf("0x%X: %v", f, err)
				}
				spectest.CheckServer(t, srv, mwo.Definition, f)
			}
		}
	}
	for _, f := range []uint32{appliance.WasherFeatureSpin, appliance.WasherFeatureRinse, appliance.WasherFeatureSpin | appliance.WasherFeatureRinse} {
		srv, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
			Features: f, SpinSpeeds: []string{"Off", "800"}, SupportedRinses: []appliance.NumberOfRinses{lwc.NumberOfRinsesNormal}, NumberOfRinses: lwc.NumberOfRinsesNormal,
		})
		if err != nil {
			t.Fatalf("0x%X: %v", f, err)
		}
		spectest.CheckServer(t, srv, lwc.Definition, f)
	}
	dryer, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{ldc.DrynessLevelNormal}})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, dryer, ldc.Definition, 0)
}

// TestParityMatterJS_ApplianceSnapshot pins the snapshot facts the servers
// rest on: the constraints chip's rules sit beside, the command fields and
// their defaults, and the device types that carry each cluster.
func TestParityMatterJS_ApplianceSnapshot(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, appliance.ClusterIDMicrowaveOvenControl)
	if a := js.Attribute(t, "MaxCookTime"); a.Constraint != "1 to 86400" || a.Quality != "F" {
		t.Errorf("MaxCookTime %+v", a)
	}
	if a := js.Attribute(t, "CookTime"); a.Constraint != "1 to maxCookTime" {
		t.Errorf("CookTime %+v", a)
	}
	if c := js.Command(t, "AddMoreTime"); c.Conformance != "O" || c.Response != "status" {
		t.Errorf("AddMoreTime %+v", c)
	}
	if c := js.Command(t, "SetCookingParameters"); c.Conformance != "M" || c.Response != "status" {
		t.Errorf("SetCookingParameters %+v", c)
	}
	w := paritytest.ClusterSnapshot(t, appliance.ClusterIDLaundryWasherControls)
	if a := w.Attribute(t, "SpinSpeedCurrent"); a.Constraint != "max 15" || a.Access != "RW VO" || a.Quality != "X" {
		t.Errorf("SpinSpeedCurrent %+v", a)
	}
	if a := w.Attribute(t, "SupportedRinses"); a.Constraint != "max 4" {
		t.Errorf("SupportedRinses %+v", a)
	}
	d := paritytest.ClusterSnapshot(t, appliance.ClusterIDLaundryDryerControls)
	if a := d.Attribute(t, "SelectedDrynessLevel"); a.Access != "RW VO" || a.Quality != "X" {
		t.Errorf("SelectedDrynessLevel %+v", a)
	}
	if a := d.Attribute(t, "SupportedDrynessLevels"); a.Constraint != "1 to 4" {
		t.Errorf("SupportedDrynessLevels %+v", a)
	}
	for _, c := range []struct {
		dt, cluster uint32
		required    bool
	}{
		{0x0079, appliance.ClusterIDMicrowaveOvenControl, true},   // MicrowaveOven
		{0x0073, appliance.ClusterIDLaundryWasherControls, false}, // LaundryWasher
		{0x007C, appliance.ClusterIDLaundryDryerControls, false},  // LaundryDryer
	} {
		if allowed, known := schema.DeviceTypeAllowsServerCluster(c.dt, c.cluster); !allowed || !known {
			t.Errorf("device type 0x%04X does not offer 0x%04X", c.dt, c.cluster)
		}
		if schema.DeviceTypeRequiresServerCluster(c.dt, c.cluster) != c.required {
			t.Errorf("device type 0x%04X requires 0x%04X: want %v", c.dt, c.cluster, c.required)
		}
	}
	if appliance.DefaultCookTime != 30 {
		t.Error("chip's kDefaultCookTimeSec is 30")
	}
	// The snapshot's CookTime field default agrees with chip's.
	for _, f := range mwo.Definition.Command(mwo.CmdSetCookingParameters, spec.Request).Fields {
		if f.Name == "CookTime" && f.Default != float64(appliance.DefaultCookTime) {
			t.Errorf("CookTime default %v", f.Default)
		}
	}
}
