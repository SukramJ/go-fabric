// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/pump"
)

// TestReadServesOnlyAdmittedModes (R2-C2-wire-1553): an OperationMode,
// ControlMode or their Effective twin the host reports that the selection
// does not admit — outside the enum, or a value whose feature is not
// selected — is not put on the wire; the last admitted value is served,
// the first value the selection admits before any.
func TestReadServesOnlyAdmittedModes(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv, err := pump.NewServer(pump.Config{
		Source: d, Features: pump.FeatureConstantFlow | pump.FeatureLocalOperation,
		Optional: pump.OptionalControlMode,
	})
	if err != nil {
		t.Fatal(err)
	}
	report := func(op pump.OperationMode, ctl pump.ControlMode) {
		d.mu.Lock()
		d.st.OperationMode, d.st.EffectiveOperationMode = op, op
		d.st.ControlMode, d.st.EffectiveControlMode = ctl, ctl
		d.mu.Unlock()
	}
	attrs := []uint32{pump.AttrOperationMode, pump.AttrEffectiveOperationMode, pump.AttrControlMode, pump.AttrEffectiveControlMode}
	check := func(when string, op, ctl uint8) {
		t.Helper()
		for _, id := range attrs {
			want := op
			if id == pump.AttrControlMode || id == pump.AttrEffectiveControlMode {
				want = ctl
			}
			if v, _ := srv.MatterRead(id); v != want {
				t.Errorf("%s: attribute 0x%04X reads %v, want %d", when, id, v, want)
			}
		}
	}
	// 9 is in neither enum; ConstantSpeed (0) needs SPD. Before any
	// admitted value: Normal (0) and ConstantFlow (3), the first the
	// selection admits.
	report(9, 0)
	check("before any admitted value", 0, 3)
	report(3, 3) // Local (LOCAL), ConstantFlow (FLW)
	check("admitted values", 3, 3)
	report(1, 9) // Minimum needs SPD; 9 is undefined
	check("after admitted values", 3, 3)
}
