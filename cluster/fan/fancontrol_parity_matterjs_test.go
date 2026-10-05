// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/fan"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// The FanControl entry of parity/schema.json, the matter.js HEAD pin.
type (
	snapElement = *paritytest.Element
	snapCluster = *paritytest.Cluster
)

func fanSnapshot(t *testing.T) snapCluster {
	t.Helper()
	return paritytest.ClusterSnapshot(t, fan.ClusterID)
}

// conformance is the shared matter.js conformance evaluator.
var conformance = paritytest.Conformance

var featureByName = map[string]fan.Feature{
	"SPD": fan.FeatureMultiSpeed, "AUT": fan.FeatureAuto, "RCK": fan.FeatureRocking,
	"WND": fan.FeatureWind, "STEP": fan.FeatureStep, "DIR": fan.FeatureAirflowDirection,
}

func TestParityMatterJS_FanControlRevisionIDsAndFeatures(t *testing.T) {
	t.Parallel()
	js := fanSnapshot(t)
	if fan.Revision() != js.Revision {
		t.Errorf("Revision = %d, want %d", fan.Revision(), js.Revision)
	}
	attrs := map[string]uint32{
		"FanMode": fan.AttrFanMode, "FanModeSequence": fan.AttrFanModeSequence, "PercentSetting": fan.AttrPercentSetting,
		"PercentCurrent": fan.AttrPercentCurrent, "SpeedMax": fan.AttrSpeedMax, "SpeedSetting": fan.AttrSpeedSetting,
		"SpeedCurrent": fan.AttrSpeedCurrent, "RockSupport": fan.AttrRockSupport, "RockSetting": fan.AttrRockSetting,
		"WindSupport": fan.AttrWindSupport, "WindSetting": fan.AttrWindSetting, "AirflowDirection": fan.AttrAirflowDirection,
	}
	writable := []uint32{fan.AttrFanMode, fan.AttrPercentSetting, fan.AttrSpeedSetting, fan.AttrRockSetting, fan.AttrWindSetting, fan.AttrAirflowDirection}
	for _, a := range js.Attributes {
		if a.ID >= 0xFFF0 {
			continue
		}
		id, ok := attrs[a.Name]
		if !ok || id != a.ID {
			t.Errorf("attribute %s (0x%04X) has constant 0x%04X (known %v)", a.Name, a.ID, id, ok)
		}
		if strings.HasPrefix(a.Access, "RW") != slices.Contains(writable, a.ID) {
			t.Errorf("%s access %q disagrees with the server's writable set", a.Name, a.Access)
		}
		if strings.HasPrefix(a.Access, "RW") && !strings.HasSuffix(a.Access, "VO") {
			t.Errorf("%s access %q: the server writes everything at the Operate default", a.Name, a.Access)
		}
	}
	if len(js.Commands) != 1 || js.Commands[0].Name != "Step" || js.Commands[0].ID != fan.CmdStep ||
		js.Commands[0].Conformance != "STEP" || js.Commands[0].Response != "status" {
		t.Errorf("matter.js commands %+v; the server serves Step (0, STEP, status)", js.Commands)
	}
	if len(js.Events) != 0 {
		t.Errorf("matter.js FanControl has %d events; the server lists none", len(js.Events))
	}
	if len(js.Features) != len(featureByName) {
		t.Errorf("matter.js has %d features, the server names %d", len(js.Features), len(featureByName))
	}
	for _, f := range js.Features {
		if want, ok := featureByName[f.Name]; !ok || uint32(want) != 1<<f.Bit || f.Conformance != "O" {
			t.Errorf("feature %+v has no matching optional constant", f)
		}
	}
}

// TestParityMatterJS_FanControlConformance builds every feature
// combination the constructor accepts and checks the attribute and
// command lists against each element's conformance.
func TestParityMatterJS_FanControlConformance(t *testing.T) {
	t.Parallel()
	js := fanSnapshot(t)
	built := 0
	for f := range fan.Feature(1 << 6) {
		seq := fan.SequenceOffLowMedHigh
		if f&fan.FeatureAuto != 0 {
			seq = fan.SequenceOffLowMedHighAuto
		}
		srv, err := fan.NewServer(fan.Config{
			Source: &device{}, Features: f, Sequence: seq, SpeedMax: 5,
			RockSupport: fan.RockRound, WindSupport: fan.WindSleep,
		})
		if err != nil {
			t.Fatalf("features 0x%02X rejected: %v", f, err)
		}
		built++
		names := map[string]bool{}
		for n, bit := range featureByName {
			names[n] = f&bit != 0
		}
		check := func(kind string, elems []snapElement, served []uint32) {
			for _, e := range elems {
				if e.ID >= 0xFFF0 {
					continue
				}
				required, allowed := conformance(e.Conformance, names)
				has := slices.Contains(served, e.ID)
				if required && !has {
					t.Errorf("features 0x%02X: mandatory %s %s (%q) missing", f, kind, e.Name, e.Conformance)
				}
				if has && !allowed {
					t.Errorf("features 0x%02X: %s %s (%q) served but not allowed", f, kind, e.Name, e.Conformance)
				}
			}
		}
		check("attribute", js.Attributes, srv.MatterAttributes())
		check("command", js.Commands, srv.MatterAcceptedCommands())
		if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(f) {
			t.Errorf("FeatureMap = %v, want 0x%02X", fm, f)
		}
	}
	if built != 64 {
		t.Errorf("built %d of 64 feature combinations", built)
	}
}

// TestParityMatterJS_FanControlConstraints holds the numeric constraints
// of the snapshot against what the server accepts and reports.
func TestParityMatterJS_FanControlConstraints(t *testing.T) {
	t.Parallel()
	js := fanSnapshot(t)
	got := map[string]string{}
	for _, a := range js.Attributes {
		got[a.Name] = a.Constraint
	}
	want := map[string]string{
		"PercentSetting": "max 100", "PercentCurrent": "max 100", "SpeedMax": "1 to 100",
		"SpeedSetting": "max speedMax", "SpeedCurrent": "max speedMax", "RockSupport": "min 1", "WindSupport": "min 1",
	}
	for name, c := range want {
		if got[name] != c {
			t.Errorf("%s constraint = %q in matter.js, the server enforces %q", name, got[name], c)
		}
	}
	ctx := context.Background()
	srv := newFull(t, &device{})
	for _, w := range []struct {
		attr uint32
		ok   uint64
		bad  uint64
	}{
		{fan.AttrPercentSetting, 100, 101},
		{fan.AttrSpeedSetting, 10, 11}, // SpeedMax 10
	} {
		if err := srv.MatterWrite(ctx, w.attr, w.ok); err != nil {
			t.Errorf("0x%04X = %d rejected: %v", w.attr, w.ok, err)
		}
		if err := srv.MatterWrite(ctx, w.attr, w.bad); statusOf(t, err) != im.StatusConstraintError {
			t.Errorf("0x%04X = %d: %v, want ConstraintError", w.attr, w.bad, err)
		}
	}
	for _, sm := range []uint8{0, 101} {
		if _, err := fan.NewServer(fan.Config{Source: &device{}, Features: fan.FeatureMultiSpeed, Sequence: fan.SequenceOffHigh, SpeedMax: sm}); err == nil {
			t.Errorf("SpeedMax %d accepted against \"1 to 100\"", sm)
		}
	}
}

// TestParityMatterJS_FanControlDeviceTypes pins the three device types
// that mandate the cluster, and ExtractorHood's exclusion of RCK, WND and
// DIR (extractor-hood.element.ts: Requirement FanControl with RCK / WND /
// DIR conformance "X" — the snapshot does not carry feature requirements).
func TestParityMatterJS_FanControlDeviceTypes(t *testing.T) {
	t.Parallel()
	for id, name := range map[uint16]string{
		fan.DeviceTypeFan: "Fan", fan.DeviceTypeAirPurifier: "AirPurifier", fan.DeviceTypeExtractorHood: "ExtractorHood",
	} {
		if got, _ := schema.DeviceTypeName(uint32(id)); got != name {
			t.Errorf("device type 0x%04X is %q, want %q", id, got, name)
		}
		if !schema.DeviceTypeRequiresServerCluster(uint32(id), fan.ClusterID) {
			t.Errorf("%s does not mandate FanControl in the snapshot", name)
		}
	}
	for _, f := range []fan.Feature{fan.FeatureRocking, fan.FeatureWind, fan.FeatureAirflowDirection} {
		_, err := fan.NewServer(fan.Config{
			Source: &device{}, DeviceType: fan.DeviceTypeExtractorHood, Features: f, Sequence: fan.SequenceOffHigh,
			RockSupport: fan.RockRound, WindSupport: fan.WindSleep,
		})
		if err == nil {
			t.Errorf("ExtractorHood accepted feature 0x%02X", f)
		}
	}
}

// TestParityMatterJS_FanControlEnumValues pins the enum and bitmap values
// against fan-control.element.ts:76-118 (the snapshot carries no
// datatypes), and the Step field tags against :71-73.
func TestParityMatterJS_FanControlEnumValues(t *testing.T) {
	t.Parallel()
	got := []uint8{
		uint8(fan.FanModeOff), uint8(fan.FanModeLow), uint8(fan.FanModeMedium), uint8(fan.FanModeHigh),
		uint8(fan.FanModeOn), uint8(fan.FanModeAuto), uint8(fan.FanModeSmart),
		uint8(fan.SequenceOffLowMedHigh), uint8(fan.SequenceOffLowHigh), uint8(fan.SequenceOffLowMedHighAuto),
		uint8(fan.SequenceOffLowHighAuto), uint8(fan.SequenceOffHighAuto), uint8(fan.SequenceOffHigh),
		uint8(fan.RockLeftRight), uint8(fan.RockUpDown), uint8(fan.RockRound),
		uint8(fan.WindSleep), uint8(fan.WindNatural),
		fan.StepIncrease, fan.StepDecrease,
		uint8(fan.AirflowForward), uint8(fan.AirflowReverse),
		clusterwire.FanStepFieldDirection, clusterwire.FanStepFieldWrap, clusterwire.FanStepFieldLowestOff,
	}
	want := []uint8{0, 1, 2, 3, 4, 5, 6, 0, 1, 2, 3, 4, 5, 1, 2, 4, 1, 2, 0, 1, 0, 1, 0, 1, 2}
	if !slices.Equal(got, want) {
		t.Errorf("enum values %v, want %v", got, want)
	}
}
