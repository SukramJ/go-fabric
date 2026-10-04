// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// The SmokeCoAlarm entry of parity/schema.json, the matter.js HEAD pin.
type (
	snapElement = paritytest.Element
	snapCluster = paritytest.Cluster
)

func smokeSnapshot(t *testing.T) snapCluster {
	t.Helper()
	return paritytest.ClusterSnapshot(t, alarm.ClusterID)
}

// conformance is the shared matter.js conformance evaluator.
var conformance = paritytest.Conformance

func featureNames(f alarm.Feature) map[string]bool {
	return map[string]bool{
		"SMOKE": f&alarm.FeatureSmokeAlarm != 0,
		"CO":    f&alarm.FeatureCOAlarm != 0,
	}
}

func byName(t *testing.T, elems []snapElement, name string) snapElement {
	t.Helper()
	for _, e := range elems {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("matter.js SmokeCoAlarm has no element %q", name)
	return snapElement{}
}

func TestParityMatterJS_SmokeCoAlarmRevisionAndIDs(t *testing.T) {
	t.Parallel()
	js := smokeSnapshot(t)
	if alarm.Revision() != js.Revision {
		t.Errorf("Revision = %d, want %d", alarm.Revision(), js.Revision)
	}
	for name, id := range map[string]uint32{
		"ExpressedState": alarm.AttrExpressedState, "SmokeState": alarm.AttrSmokeState,
		"CoState": alarm.AttrCOState, "BatteryAlert": alarm.AttrBatteryAlert,
		"DeviceMuted": alarm.AttrDeviceMuted, "TestInProgress": alarm.AttrTestInProgress,
		"HardwareFaultAlert": alarm.AttrHardwareFaultAlert, "EndOfServiceAlert": alarm.AttrEndOfServiceAlert,
		"InterconnectSmokeAlarm": alarm.AttrInterconnectSmokeAlarm, "InterconnectCoAlarm": alarm.AttrInterconnectCOAlarm,
		"ContaminationState": alarm.AttrContaminationState, "SmokeSensitivityLevel": alarm.AttrSmokeSensitivityLevel,
		"ExpiryDate": alarm.AttrExpiryDate, "Unmounted": alarm.AttrUnmounted,
	} {
		if got := byName(t, js.Attributes, name).ID; got != id {
			t.Errorf("attribute %s = 0x%04X, want 0x%04X", name, id, got)
		}
	}
	for name, id := range map[string]uint32{
		"SmokeAlarm": alarm.EventSmokeAlarm, "CoAlarm": alarm.EventCOAlarm, "LowBattery": alarm.EventLowBattery,
		"HardwareFault": alarm.EventHardwareFault, "EndOfService": alarm.EventEndOfService,
		"SelfTestComplete": alarm.EventSelfTestComplete, "AlarmMuted": alarm.EventAlarmMuted,
		"MuteEnded": alarm.EventMuteEnded, "InterconnectSmokeAlarm": alarm.EventInterconnectSmokeAlarm,
		"InterconnectCoAlarm": alarm.EventInterconnectCOAlarm, "AllClear": alarm.EventAllClear,
	} {
		if got := byName(t, js.Events, name).ID; got != id {
			t.Errorf("event %s = 0x%02X, want 0x%02X", name, id, got)
		}
	}
	cmd := byName(t, js.Commands, "SelfTestRequest")
	if cmd.ID != alarm.CmdSelfTestRequest || cmd.Response != "status" || cmd.Conformance != "O" {
		t.Errorf("SelfTestRequest = %+v; the server treats it as optional id 0 with a status response", cmd)
	}
	for _, f := range js.Features {
		want := map[string]alarm.Feature{"SMOKE": alarm.FeatureSmokeAlarm, "CO": alarm.FeatureCOAlarm}[f.Name]
		if want == 0 || uint32(want) != 1<<f.Bit {
			t.Errorf("feature %s bit %d has no matching constant", f.Name, f.Bit)
		}
	}
	// Writability: only SmokeSensitivityLevel is "RW".
	for _, a := range js.Attributes {
		if a.Access == "" {
			continue
		}
		writable := strings.HasPrefix(a.Access, "RW")
		if writable != (a.ID == alarm.AttrSmokeSensitivityLevel) {
			t.Errorf("%s access %q disagrees with the server's single writable attribute", a.Name, a.Access)
		}
		if writable && !strings.Contains(a.Access, "VM") {
			t.Errorf("%s access %q: the server asks for Manage on write", a.Name, a.Access)
		}
	}
}

// TestParityMatterJS_SmokeCoAlarmConformance runs every feature and
// optional-attribute combination the constructor accepts and checks the
// published attribute and event lists against the matter.js conformance
// of each element: every mandatory element is there, nothing disallowed
// is.
func TestParityMatterJS_SmokeCoAlarmConformance(t *testing.T) {
	t.Parallel()
	js := smokeSnapshot(t)
	optionals := []alarm.Optional{
		alarm.OptionalDeviceMuted, alarm.OptionalInterconnectSmokeAlarm, alarm.OptionalInterconnectCOAlarm,
		alarm.OptionalContaminationState, alarm.OptionalSmokeSensitivityLevel, alarm.OptionalExpiryDate,
		alarm.OptionalUnmounted,
	}
	built := 0
	for features := alarm.Feature(0); features <= 3; features++ {
		for mask := range 1 << len(optionals) {
			var opt alarm.Optional
			for i, o := range optionals {
				if mask&(1<<i) != 0 {
					opt |= o
				}
			}
			srv, err := alarm.NewServer(alarm.Config{Source: &device{}, Features: features, Optional: opt})
			if err != nil {
				continue
			}
			built++
			names := featureNames(features)
			if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(features) {
				t.Fatalf("FeatureMap = %v, want %d", fm, features)
			}
			check := func(kind string, elems []snapElement, served []uint32) {
				for _, e := range elems {
					if e.ID >= 0xFFF0 {
						continue
					}
					required, allowed := conformance(e.Conformance, names)
					has := slices.Contains(served, e.ID)
					if required && !has {
						t.Errorf("features %d optionals 0x%X: mandatory %s %s (%q) not published", features, opt, kind, e.Name, e.Conformance)
					}
					if has && !allowed {
						t.Errorf("features %d optionals 0x%X: %s %s (%q) published but not allowed", features, opt, kind, e.Name, e.Conformance)
					}
				}
			}
			check("attribute", js.Attributes, srv.MatterAttributes())
			check("event", js.Events, srv.MatterEvents())
		}
	}
	if built == 0 {
		t.Fatal("no configuration was accepted")
	}
	// The "O.a+" choice: no configuration without a feature is accepted.
	if _, err := alarm.NewServer(alarm.Config{Source: &device{}}); err == nil {
		t.Error("a SmokeCoAlarm without SMOKE and CO was accepted")
	}
}

// TestParityMatterJS_SmokeCoAlarmEventPriorities checks every emitted
// event carries the priority matter.js declares for it.
func TestParityMatterJS_SmokeCoAlarmEventPriorities(t *testing.T) {
	t.Parallel()
	js := smokeSnapshot(t)
	want := map[uint32]contract.EventPriority{}
	for _, e := range js.Events {
		switch e.Priority {
		case "critical":
			want[e.ID] = contract.EventPriorityCritical
		case "info":
			want[e.ID] = contract.EventPriorityInfo
		default:
			want[e.ID] = contract.EventPriorityDebug
		}
	}
	d := &device{}
	srv := newFull(t, d)
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	d.set(func(s *alarm.State) {
		*s = alarm.State{
			SmokeState: alarm.AlarmWarning, COState: alarm.AlarmWarning, BatteryAlert: alarm.AlarmWarning,
			DeviceMuted: alarm.Muted, HardwareFaultAlert: true, EndOfServiceAlert: alarm.EndOfServiceExpired,
			InterconnectSmokeAlarm: alarm.AlarmWarning, InterconnectCOAlarm: alarm.AlarmWarning, TestInProgress: true,
		}
	})
	srv.Refresh()
	d.set(func(s *alarm.State) { *s = alarm.State{} })
	srv.Refresh()
	seen := map[uint32]bool{}
	for _, e := range rec.take() {
		seen[e.event] = true
		if e.priority != want[e.event] {
			t.Errorf("event 0x%02X priority %d, want %d (matter.js)", e.event, e.priority, want[e.event])
		}
	}
	for _, id := range srv.MatterEvents() {
		if !seen[id] {
			t.Errorf("event 0x%02X is listed but no transition emitted it", id)
		}
	}
}

// TestParityMatterJS_SmokeCoAlarmDeviceType pins the device type the
// server is mounted on: SmokeCoAlarm (0x0076) mandates the cluster.
func TestParityMatterJS_SmokeCoAlarmDeviceType(t *testing.T) {
	t.Parallel()
	if name, _ := schema.DeviceTypeName(uint32(alarm.DeviceTypeSmokeCoAlarm)); name != "SmokeCoAlarm" {
		t.Errorf("device type 0x%04X is %q", alarm.DeviceTypeSmokeCoAlarm, name)
	}
	if !schema.DeviceTypeRequiresServerCluster(uint32(alarm.DeviceTypeSmokeCoAlarm), alarm.ClusterID) {
		t.Error("SmokeCoAlarm does not mandate the SmokeCoAlarm cluster in the snapshot")
	}
}

// TestParityMatterJS_SmokeCoAlarmEnumValues pins the enum values
// against smoke-co-alarm-cluster.element.ts:75-120 (the snapshot does
// not carry datatypes).
func TestParityMatterJS_SmokeCoAlarmEnumValues(t *testing.T) {
	t.Parallel()
	got := []uint8{
		uint8(alarm.AlarmNormal), uint8(alarm.AlarmWarning), uint8(alarm.AlarmCritical),
		uint8(alarm.SensitivityHigh), uint8(alarm.SensitivityStandard), uint8(alarm.SensitivityLow),
		uint8(alarm.ExpressedNormal), uint8(alarm.ExpressedSmokeAlarm), uint8(alarm.ExpressedCOAlarm),
		uint8(alarm.ExpressedBatteryAlert), uint8(alarm.ExpressedTesting), uint8(alarm.ExpressedHardwareFault),
		uint8(alarm.ExpressedEndOfService), uint8(alarm.ExpressedInterconnectSmoke), uint8(alarm.ExpressedInterconnectCO),
		uint8(alarm.ExpressedInoperative),
		uint8(alarm.NotMuted), uint8(alarm.Muted),
		uint8(alarm.EndOfServiceNormal), uint8(alarm.EndOfServiceExpired),
		uint8(alarm.ContaminationNormal), uint8(alarm.ContaminationLow), uint8(alarm.ContaminationWarning), uint8(alarm.ContaminationCritical),
	}
	want := []uint8{0, 1, 2, 0, 1, 2, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 0, 1, 0, 1, 2, 3}
	if !slices.Equal(got, want) {
		t.Errorf("enum values %v, want %v", got, want)
	}
}
