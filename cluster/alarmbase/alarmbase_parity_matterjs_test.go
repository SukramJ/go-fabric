// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package alarmbase_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	rfa "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tma "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// allThresholds answers every TemperatureAlarm threshold, in the order the
// constraints ask for (critical under < major under < minor under < minor
// over < major over < critical over).
var allThresholds = map[uint32]int16{
	tma.AttrCriticalUnderTemperatureThreshold: -1000,
	tma.AttrMajorUnderTemperatureThreshold:    -500,
	tma.AttrMinorUnderTemperatureThreshold:    0,
	tma.AttrMinorOverTemperatureThreshold:     5000,
	tma.AttrMajorOverTemperatureThreshold:     7000,
	tma.AttrCriticalOverTemperatureThreshold:  9000,
}

// TestParityMatterJS_AlarmBaseServers builds every derivation with every
// feature selection its definition allows (the TemperatureAlarm *ADJ
// features excepted, see ErrAdjustableThresholds) and both
// ModifyEnabledAlarms choices where that command is optional, and holds
// each server against the matter.js conformance of its definition.
func TestParityMatterJS_AlarmBaseServers(t *testing.T) {
	t.Parallel()
	for _, d := range []struct {
		def      *spec.Cluster
		new      func(alarmbase.Config) (*alarmbase.Server, error)
		features []uint32
		modify   []bool
	}{
		{dwa.Definition, alarmbase.NewDishwasherAlarm, []uint32{0, uint32(dwa.FeatureReset)}, []bool{false, true}},
		{rfa.Definition, alarmbase.NewRefrigeratorAlarm, []uint32{0}, []bool{false}},
		{tma.Definition, alarmbase.NewTemperatureAlarm, temperatureSelections(), []bool{false, true}},
	} {
		for _, f := range d.features {
			for _, m := range d.modify {
				srv, err := d.new(alarmbase.Config{Features: f, ModifyEnabledAlarms: m, Supported: 1, Latch: 1, Mask: 1, Thresholds: allThresholds})
				if err != nil {
					t.Fatalf("%s 0x%X modify=%v: %v", d.def.Name, f, m, err)
				}
				spectest.CheckServer(t, srv, d.def, f)
			}
		}
	}
}

// temperatureSelections lists the TemperatureAlarm FeatureMaps without the
// *ADJ bits that spec.CheckFeatures admits.
func temperatureSelections() []uint32 {
	base := []uint32{0, uint32(tma.FeatureReset)}
	var out []uint32
	for _, r := range base {
		for _, side := range []uint32{uint32(tma.FeatureOverTemperature), uint32(tma.FeatureUnderTemperature), uint32(tma.FeatureOverTemperature | tma.FeatureUnderTemperature)} {
			for _, lvl := range []uint32{0, uint32(tma.FeatureMajorThreshold), uint32(tma.FeatureMajorThreshold | tma.FeatureMinorThreshold)} {
				f := r | side | lvl
				if spec.CheckFeatures(tma.Definition, f) == nil {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// TestParityMatterJS_AlarmBaseDisallowed: the snapshot disallows Reset and
// ModifyEnabledAlarms on RefrigeratorAlarm ("X"); the server refuses them.
func TestParityMatterJS_AlarmBaseDisallowed(t *testing.T) {
	t.Parallel()
	if _, err := alarmbase.NewRefrigeratorAlarm(alarmbase.Config{Features: alarmbase.FeatureReset}); err == nil {
		t.Error("RefrigeratorAlarm with Reset built")
	}
	if _, err := alarmbase.NewRefrigeratorAlarm(alarmbase.Config{ModifyEnabledAlarms: true}); err == nil {
		t.Error("RefrigeratorAlarm with ModifyEnabledAlarms built")
	}
	if _, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: uint32(tma.FeatureOverTemperature | tma.FeatureOverCriticalAdjustable), Thresholds: allThresholds}); err == nil {
		t.Error("TemperatureAlarm with OCRIADJ built")
	}
}

// TestParityMatterJS_AlarmBaseSnapshot pins the snapshot facts the server
// rests on: all three derive from AlarmBase with the same Mask, Latch,
// State, Supported ids, Reset (RESET) and ModifyEnabledAlarms answered with
// a status, an info Notify of four AlarmBitmap fields; the bitmaps are the
// derivations' own; and the device types that offer each cluster.
func TestParityMatterJS_AlarmBaseSnapshot(t *testing.T) {
	t.Parallel()
	for _, id := range []uint32{alarmbase.ClusterIDDishwasherAlarm, alarmbase.ClusterIDRefrigeratorAlarm, alarmbase.ClusterIDTemperatureAlarm} {
		js := paritytest.ClusterSnapshot(t, id)
		if js.Base != "AlarmBase" {
			t.Errorf("0x%04X derives from %q", id, js.Base)
		}
		for name, want := range map[string]uint32{"Mask": alarmbase.AttrMask, "Latch": alarmbase.AttrLatch, "State": alarmbase.AttrState, "Supported": alarmbase.AttrSupported} {
			if a := js.Attribute(t, name); a.ID != want {
				t.Errorf("0x%04X %s id %d", id, name, a.ID)
			}
		}
		if a := js.Attribute(t, "Latch"); a.Conformance != "RESET" || a.Quality != "F" {
			t.Errorf("0x%04X Latch %+v", id, a)
		}
		if c := js.Command(t, "Reset"); c.ID != alarmbase.CmdReset || c.Conformance != "RESET" || c.Response != "status" {
			t.Errorf("0x%04X Reset %+v", id, c)
		}
		if c := js.Command(t, "ModifyEnabledAlarms"); c.ID != alarmbase.CmdModifyEnabledAlarms {
			t.Errorf("0x%04X ModifyEnabledAlarms %+v", id, c)
		}
		if e := js.Event(t, "Notify"); e.ID != alarmbase.EventNotify || e.Priority != "info" || e.Conformance != "M" {
			t.Errorf("0x%04X Notify %+v", id, e)
		}
	}
	for _, c := range []struct{ dt, cluster uint32 }{
		{0x0075, alarmbase.ClusterIDDishwasherAlarm},   // Dishwasher
		{0x0070, alarmbase.ClusterIDRefrigeratorAlarm}, // Refrigerator
		{0x0071, alarmbase.ClusterIDTemperatureAlarm},  // TemperatureControlledCabinet
	} {
		if allowed, known := schema.DeviceTypeAllowsServerCluster(c.dt, c.cluster); !allowed || !known {
			t.Errorf("device type 0x%04X does not offer 0x%04X", c.dt, c.cluster)
		}
	}
	if dwa.AlarmDoorError != 1<<2 || rfa.AlarmDoorOpen != 1<<0 || tma.AlarmCriticalUnderTemperatureAlarm != 1<<5 {
		t.Error("AlarmBitmap bits moved")
	}
}
