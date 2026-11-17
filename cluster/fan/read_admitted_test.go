// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
)

// TestReadServesOnlyAdmittedFanModeAndAirflow (R2-P6-wire-1564): a
// FanMode or AirflowDirection the host reports that the selection does not
// admit — outside the enum, or Auto without the Auto feature — is not put
// on the wire; the last admitted value is served, Off and Forward before
// any.
func TestReadServesOnlyAdmittedFanModeAndAirflow(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv, err := fan.NewServer(fan.Config{Source: d, Features: fan.FeatureAirflowDirection, Sequence: fan.SequenceOffLowMedHigh})
	if err != nil {
		t.Fatal(err)
	}
	report := func(mode fandef.FanModeEnum, dir fandef.AirflowDirectionEnum) {
		d.mu.Lock()
		d.st.FanMode, d.st.AirflowDirection = mode, dir
		d.mu.Unlock()
	}
	read := func() (any, any) {
		m, _ := srv.MatterRead(fan.AttrFanMode)
		a, _ := srv.MatterRead(fan.AttrAirflowDirection)
		return m, a
	}
	report(9, 5)
	if m, a := read(); m != uint8(0) || a != uint8(0) {
		t.Errorf("FanMode 9, AirflowDirection 5 before any admitted value read %v, %v; want Off, Forward", m, a)
	}
	report(fandef.FanModeHigh, fandef.AirflowDirectionReverse)
	if m, a := read(); m != uint8(fandef.FanModeHigh) || a != uint8(fandef.AirflowDirectionReverse) {
		t.Errorf("High, Reverse read %v, %v", m, a)
	}
	report(fandef.FanModeAuto, 2) // Auto without the Auto feature
	if m, a := read(); m != uint8(fandef.FanModeHigh) || a != uint8(fandef.AirflowDirectionReverse) {
		t.Errorf("Auto without AUT and AirflowDirection 2 read %v, %v; want the last admitted High, Reverse", m, a)
	}
}
