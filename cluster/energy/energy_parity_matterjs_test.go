// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	mtrid "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	matterparity "github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_EnergyServers builds every feature selection of both
// servers and holds each against the matter.js conformance of its
// definition: FeatureMap, ClusterRevision, the attribute, command and
// event lists, the privileges and the read-only writes
// (spectest.CheckServer).
func TestParityMatterJS_EnergyServers(t *testing.T) {
	t.Parallel()
	for features := range energy.WaterHeaterFeature(1 << 2) {
		srv, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
			Features: features, HeaterTypes: energy.HeatSourceImmersionElement1, Booster: &booster{},
		})
		if err != nil {
			t.Fatalf("WaterHeaterManagement 0b%02b: %v", features, err)
		}
		spectest.CheckServer(t, srv, whm.Definition, uint32(features))
	}
	for features := energy.PreferenceFeature(1); features < 1<<2; features++ {
		srv, err := energy.NewEnergyPreference(preferenceConfig(features))
		if err != nil {
			t.Fatalf("EnergyPreference 0b%02b: %v", features, err)
		}
		spectest.CheckServer(t, srv, ep.Definition, uint32(features))
	}
	// BALA and LPMS are "O.a+": at least one.
	if _, err := energy.NewEnergyPreference(preferenceConfig(0)); err == nil {
		t.Error("EnergyPreference without a feature was built")
	}
}

// TestParityMatterJS_EnergySnapshot pins the snapshot facts the servers
// rest on: ids, revisions, the enum and bitmap values, the list
// constraints, the writable indexes' access, the commands' responses, and
// the device types that offer the clusters.
func TestParityMatterJS_EnergySnapshot(t *testing.T) {
	t.Parallel()
	w := paritytest.ClusterSnapshot(t, energy.ClusterIDWaterHeaterManagement)
	if w.Revision != whm.Revision {
		t.Errorf("WaterHeaterManagement revision %d, generated %d", w.Revision, whm.Revision)
	}
	for _, name := range []string{"Boost", "CancelBoost"} {
		if c := w.Command(t, name); c.Response != "status" || c.Conformance != "M" {
			t.Errorf("%s %+v", name, c)
		}
	}
	for _, name := range []string{"BoostStarted", "BoostEnded"} {
		if e := w.Event(t, name); e.Conformance != "M" {
			t.Errorf("%s %+v", name, e)
		}
	}
	if a := w.Attribute(t, "TankPercentage"); a.Conformance != "TP" || a.Type != "percent" {
		t.Errorf("TankPercentage %+v", a)
	}
	whmEnums := snapshotDatatypes(t, energy.ClusterIDWaterHeaterManagement)
	expectValues(t, "BoostStateEnum", whmEnums["BoostStateEnum"], map[string]uint64{
		"Inactive": uint64(energy.BoostStateInactive), "Active": uint64(energy.BoostStateActive),
	})
	expectValues(t, "WaterHeaterHeatSourceBitmap", whmEnums["WaterHeaterHeatSourceBitmap"], map[string]uint64{
		"ImmersionElement1": bit(energy.HeatSourceImmersionElement1), "ImmersionElement2": bit(energy.HeatSourceImmersionElement2),
		"HeatPump": bit(energy.HeatSourceHeatPump), "Boiler": bit(energy.HeatSourceBoiler), "Other": bit(energy.HeatSourceOther),
	})

	p := paritytest.ClusterSnapshot(t, energy.ClusterIDEnergyPreference)
	if p.Revision != ep.Revision {
		t.Errorf("EnergyPreference revision %d, generated %d", p.Revision, ep.Revision)
	}
	for _, name := range []string{"CurrentEnergyBalance", "CurrentLowPowerModeSensitivity"} {
		if a := p.Attribute(t, name); a.Access != "RW VO" || a.Type != "uint8" {
			t.Errorf("%s %+v", name, a)
		}
	}
	for name, want := range map[string]string{"EnergyBalances": "2 to 10", "LowPowerModeSensitivities": "2 to 10", "EnergyPriorities": "2"} {
		if a := p.Attribute(t, name); a.Constraint != want {
			t.Errorf("%s constraint %q, want %q", name, a.Constraint, want)
		}
	}
	expectValues(t, "EnergyPriorityEnum", snapshotDatatypes(t, energy.ClusterIDEnergyPreference)["EnergyPriorityEnum"], map[string]uint64{
		"Comfort": uint64(energy.PriorityComfort), "Speed": uint64(energy.PrioritySpeed),
		"Efficiency": uint64(energy.PriorityEfficiency), "WaterConsumption": uint64(energy.PriorityWaterConsumption),
	})

	for _, c := range []struct{ deviceType, cluster uint32 }{
		{0x050F, energy.ClusterIDWaterHeaterManagement}, // WaterHeater
		{0x0301, energy.ClusterIDEnergyPreference},      // Thermostat
	} {
		if allowed, known := schema.DeviceTypeAllowsServerCluster(c.deviceType, c.cluster); !allowed || !known {
			t.Errorf("device type 0x%04X does not offer 0x%04X", c.deviceType, c.cluster)
		}
	}
}

// TestParityChip_EnergyPreferenceIndexWrites: a Current… write at or
// beyond its list's length is CONSTRAINT_ERROR and leaves the value; the
// last index is accepted (connectedhomeip EnergyPreferenceCluster.cpp:
// 113-116 at the harness pin; TC_EPREF_2_1.py steps 4a/4b, 7a/7b).
func TestParityChip_EnergyPreferenceIndexWrites(t *testing.T) {
	t.Parallel()
	cfg := preferenceConfig(energy.PreferenceFeatureEnergyBalance | energy.PreferenceFeatureLowPowerModeSensitivity)
	srv, err := energy.NewEnergyPreference(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		attr uint32
		n    int
	}{
		{ep.AttrCurrentEnergyBalance, len(cfg.EnergyBalances)},
		{ep.AttrCurrentLowPowerModeSensitivity, len(cfg.LowPowerModeSensitivities)},
	} {
		for _, bad := range []uint64{uint64(c.n), uint64(c.n) + 1, 255} {
			wantStatus(t, srv.MatterWrite(context.Background(), c.attr, bad), im.StatusConstraintError)
		}
		if v, _ := srv.MatterRead(c.attr); v != uint8(0) {
			t.Errorf("0x%04X after refused writes = %v, want 0", c.attr, v)
		}
		if err := srv.MatterWrite(context.Background(), c.attr, uint64(c.n-1)); err != nil {
			t.Errorf("0x%04X write of the last index: %v", c.attr, err)
		}
		if v, _ := srv.MatterRead(c.attr); v != uint8(c.n-1) { //nolint:gosec // a short test list
			t.Errorf("0x%04X = %v, want %d", c.attr, v, c.n-1)
		}
	}
}

// TestParityChip_BoostFieldRules: chip's HandleBoost
// (WaterHeaterManagementCluster.cpp:152-189 at the harness pin) answers
// INVALID_COMMAND to a target percentage or reheat above 100, a reheat
// without a percentage or with OneShot, and either field without TP; the
// host is not called.
func TestParityChip_BoostFieldRules(t *testing.T) {
	t.Parallel()
	p := func(v uint8) *uint8 { return &v }
	yes := true
	for _, c := range []struct {
		name     string
		features energy.WaterHeaterFeature
		info     energy.BoostInfo
	}{
		{"percentage over 100", energy.WaterHeaterFeatureTankPercent, energy.BoostInfo{Duration: 60, TargetPercentage: p(101)}},
		{"reheat over 100", energy.WaterHeaterFeatureTankPercent, energy.BoostInfo{Duration: 60, TargetPercentage: p(100), TargetReheat: p(101)}},
		{"reheat without percentage", energy.WaterHeaterFeatureTankPercent, energy.BoostInfo{Duration: 60, TargetReheat: p(50)}},
		{"reheat with one-shot", energy.WaterHeaterFeatureTankPercent, energy.BoostInfo{Duration: 60, TargetPercentage: p(80), TargetReheat: p(50), OneShot: &yes}},
		{"percentage without TP", 0, energy.BoostInfo{Duration: 60, TargetPercentage: p(80)}},
		{"reheat without TP", 0, energy.BoostInfo{Duration: 60, TargetReheat: p(50)}},
	} {
		host := &booster{}
		srv := newWaterHeater(t, c.features, host)
		_, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, whm.BoostRequest{BoostInfo: c.info})
		wantStatus(t, err, im.StatusInvalidCommand)
		if host.boosts != 0 || srv.BoostState() != energy.BoostStateInactive {
			t.Errorf("%s: host called %d times, BoostState %d", c.name, host.boosts, srv.BoostState())
		}
	}
	// The accepted boundary: TP, percentage and reheat at 100, no OneShot.
	host := &booster{}
	srv := newWaterHeater(t, energy.WaterHeaterFeatureTankPercent, host)
	if _, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 1, TargetPercentage: p(100), TargetReheat: p(100)}}); err != nil {
		t.Errorf("boundary Boost: %v", err)
	}
}

func bit(b energy.HeatSource) uint64 {
	for n := range uint64(8) {
		if b == 1<<n {
			return n
		}
	}
	return 99
}

func expectValues(t *testing.T, what string, js, want map[string]uint64) {
	t.Helper()
	if len(js) != len(want) {
		t.Errorf("%s: snapshot has %d values, the server names %d", what, len(js), len(want))
	}
	for name, v := range want {
		if got, ok := js[name]; !ok || got != v {
			t.Errorf("%s.%s = %d, matter.js %d (present %v)", what, name, v, got, ok)
		}
	}
}

func wantStatus(t *testing.T, err error, want im.StatusCode) {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != want {
		t.Errorf("answered %v, want %s", err, want)
	}
}

// snapshotDatatypes maps each datatype of the cluster to its value (enum)
// or bit (bitmap) per name, read from the embedded snapshot.
func snapshotDatatypes(t *testing.T, clusterID uint32) map[string]map[string]uint64 {
	t.Helper()
	var s struct {
		Clusters []struct {
			ID        uint32 `json:"id"`
			Datatypes []struct {
				Name   string `json:"name"`
				Type   string `json:"type"`
				Fields []struct {
					ID         uint64 `json:"id"`
					Name       string `json:"name"`
					Constraint *struct {
						Value *uint64 `json:"value"`
					} `json:"constraint"`
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
				v := f.ID
				if f.Constraint != nil && f.Constraint.Value != nil {
					v = *f.Constraint.Value // a bitmap member's bit
				}
				out[d.Name][f.Name] = v
			}
		}
		return out
	}
	t.Fatalf("no cluster 0x%04X in the snapshot", clusterID)
	return nil
}

// TestParityMatterJS_MeterIdentification holds the MeterIdentification
// server against its definition and reads the identification back; a nil
// field is null, ProtocolVersion is served only when given, and a value
// outside the model is refused.
func TestParityMatterJS_MeterIdentification(t *testing.T) {
	t.Parallel()
	utility, serial, version := energy.MeterTypeUtility, "M-1", "DLMS 1"
	srv, err := energy.NewMeterIdentification(energy.MeterConfig{MeterType: &utility, MeterSerialNumber: &serial})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, mtrid.Definition, 0)
	for id, want := range map[uint32]any{mtrid.AttrMeterType: uint8(energy.MeterTypeUtility), mtrid.AttrMeterSerialNumber: "M-1", mtrid.AttrPointOfDelivery: nil} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("0x%04X = %v (%v), want %v", id, v, ok, want)
		}
	}
	if _, ok := srv.MatterRead(mtrid.AttrProtocolVersion); ok {
		t.Error("ProtocolVersion served without a value")
	}
	pod := "DE0001"
	srv, err = energy.NewMeterIdentification(energy.MeterConfig{PointOfDelivery: &pod, ProtocolVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(mtrid.AttrProtocolVersion); v != version {
		t.Errorf("ProtocolVersion %v", v)
	}
	if v, _ := srv.MatterRead(mtrid.AttrPointOfDelivery); v != pod {
		t.Errorf("PointOfDelivery %v", v)
	}
	bad := energy.MeterType(9)
	if _, err := energy.NewMeterIdentification(energy.MeterConfig{MeterType: &bad}); err == nil {
		t.Error("an undefined MeterType was accepted")
	}
	if allowed, known := schema.DeviceTypeAllowsServerCluster(0x0511, energy.ClusterIDMeterIdentification); !allowed || !known {
		t.Error("ElectricalUtilityMeter does not offer MeterIdentification")
	}
}
