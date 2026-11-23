// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/alarm"
	smokedef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
)

// TestReadServesOnlyDefinedEnumValues (R2-C2-wire-1553): a host value
// outside an attribute's enum never goes on the wire. The last defined
// value the host reported is served, the first value of the enum before
// any.
func TestReadServesOnlyDefinedEnumValues(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv, err := alarm.NewServer(alarm.Config{
		Source:   d,
		Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm,
		Optional: everyOptional,
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs := []uint32{
		alarm.AttrSmokeState, alarm.AttrCOState, alarm.AttrBatteryAlert, alarm.AttrDeviceMuted,
		alarm.AttrEndOfServiceAlert, alarm.AttrInterconnectSmokeAlarm, alarm.AttrInterconnectCOAlarm,
		alarm.AttrContaminationState, alarm.AttrSmokeSensitivityLevel,
	}
	read := func(id uint32) uint8 {
		v, _ := srv.MatterRead(id)
		return v.(uint8)
	}
	// Undefined before any defined value: the enum's first value.
	d.set(func(st *smokedef.State) {
		st.SmokeState, st.CoState, st.BatteryAlert, st.DeviceMuted = 7, 7, 7, 9
		st.EndOfServiceAlert, st.InterconnectSmokeAlarm, st.InterconnectCoAlarm = 9, 7, 7
		st.ContaminationState, st.SmokeSensitivityLevel = 9, 9
	})
	for _, id := range attrs {
		if v := read(id); v != 0 {
			t.Errorf("attribute 0x%04X reads %d before any defined value, want 0", id, v)
		}
	}
	// A defined value, then an undefined one: the defined one stays.
	d.set(func(st *smokedef.State) {
		st.SmokeState, st.CoState, st.BatteryAlert, st.DeviceMuted = 1, 1, 1, 1
		st.EndOfServiceAlert, st.InterconnectSmokeAlarm, st.InterconnectCoAlarm = 1, 1, 1
		st.ContaminationState, st.SmokeSensitivityLevel = 1, 1
	})
	for _, id := range attrs {
		_ = read(id)
	}
	d.set(func(st *smokedef.State) {
		st.SmokeState, st.CoState, st.BatteryAlert, st.DeviceMuted = 7, 7, 7, 9
		st.EndOfServiceAlert, st.InterconnectSmokeAlarm, st.InterconnectCoAlarm = 9, 7, 7
		st.ContaminationState, st.SmokeSensitivityLevel = 9, 9
	})
	for _, id := range attrs {
		if v := read(id); v != 1 {
			t.Errorf("attribute 0x%04X reads %d after an undefined value, want the last defined 1", id, v)
		}
	}
}
