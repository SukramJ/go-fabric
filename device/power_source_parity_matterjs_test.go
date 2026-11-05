// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The kit's PowerSource server (the generated server with matter.js's
// initialize defaults, powerSourceDefaults) after matter.js
// packages/node/test/behaviors/power-source/PowerSourceServerTest.ts and
// packages/node/src/behaviors/power-source/PowerSourceServer.ts at
// 862ad75f. The test file's one case, "successfully augments descriptor",
// builds PowerSourceServer.with(Battery, Replaceable) on a humidity sensor
// (:17-31) and stays by_design (BD-Matter-PowerSource-DeviceTypeOnPart);
// its selection, and every other one the base server's with list holds
// (Battery, Rechargeable, Wired, :12-16), is constructed here and read
// against the definition's conformance.
package device_test

import (
	"testing"

	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/device"
)

// TestPowerSourceSelectionsParityMatterJS: each selection builds on a
// HumiditySensor, its FeatureMap and attribute set hold to the
// definition (the Wired attributes with WIRED, the Battery ones with BAT,
// BatChargeState and BatFunctionalWhileCharging with RECHG,
// BatReplacementDescription and BatQuantity with REPLC), and the state
// starts at matter.js's initialize defaults (PowerSourceServer.ts:36-61):
// Status Unspecified, Description "Mains power" or "Battery power",
// BatChargeLevel Ok and BatReplaceability Unspecified with BAT,
// WiredCurrentType Ac with WIRED.
func TestPowerSourceSelectionsParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		features psdef.Feature
		initial  psdef.Patch
	}{
		{"Wired", psdef.FeatureWired, psdef.Patch{}},
		{"Battery", psdef.FeatureBattery, psdef.Patch{}},
		{"Battery, Rechargeable", psdef.FeatureBattery | psdef.FeatureRechargeable, psdef.Patch{}},
		// :19-28: the test's selection and state.
		{"Battery, Replaceable", psdef.FeatureBattery | psdef.FeatureReplaceable, psdef.Patch{
			Status: new(psdef.PowerSourceStatusActive), Order: new(uint8(1)), Description: new("aa batteries"),
			BatChargeLevel: new(psdef.BatChargeLevelOk), BatReplacementNeeded: new(false),
			BatReplaceability:         new(psdef.BatReplaceabilityUserReplaceable),
			BatReplacementDescription: new("open, replace"), BatQuantity: new(uint8(2)),
		}},
		{"Battery, Rechargeable, Replaceable", psdef.FeatureBattery | psdef.FeatureRechargeable | psdef.FeatureReplaceable, psdef.Patch{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d, err := device.NewHumiditySensor(device.Options{StableKey: device.StringKey("ps"), Name: "ps"},
				psdef.Declare{Features: c.features, Initial: c.initial})
			if err != nil {
				t.Fatal(err)
			}
			ps := d.PowerSource()
			spectest.CheckServer(t, ps.Underlying(), psdef.Definition, uint32(c.features))
			st := ps.Get()
			wired, battery := c.features&psdef.FeatureWired != 0, c.features&psdef.FeatureBattery != 0
			if c.initial.Status == nil && st.Status != psdef.PowerSourceStatusUnspecified {
				t.Errorf("Status %v, want Unspecified", st.Status)
			}
			switch {
			case c.initial.Description != nil:
				if st.Description != *c.initial.Description {
					t.Errorf("Description %q, want the set %q", st.Description, *c.initial.Description)
				}
			case wired && st.Description != "Mains power", battery && st.Description != "Battery power":
				t.Errorf("Description %q", st.Description)
			}
			if wired && st.WiredCurrentType != psdef.WiredCurrentTypeAc {
				t.Errorf("WiredCurrentType %v, want Ac", st.WiredCurrentType)
			}
			if battery {
				if st.BatChargeLevel != psdef.BatChargeLevelOk {
					t.Errorf("BatChargeLevel %v, want Ok", st.BatChargeLevel)
				}
				want := psdef.BatReplaceabilityUnspecified
				if c.initial.BatReplaceability != nil {
					want = *c.initial.BatReplaceability
				}
				if st.BatReplaceability != want {
					t.Errorf("BatReplaceability %v, want %v", st.BatReplaceability, want)
				}
			}
			if c.initial.BatQuantity != nil && (st.BatQuantity != 2 || st.BatReplacementDescription != "open, replace") {
				t.Errorf("BatQuantity %d, BatReplacementDescription %q", st.BatQuantity, st.BatReplacementDescription)
			}
		})
	}
}
