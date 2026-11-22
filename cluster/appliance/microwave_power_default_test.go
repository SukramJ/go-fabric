// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/appliance"
	mwo "thingfabric.dev/go-fabric/cluster/spec/microwaveovencontrol"
)

// TestZeroPowerSettingStartsAtTheMaximum (R2-P6-api-1592): a zero
// PowerSetting starts at the maximum power, as a zero CookTime starts at
// DefaultCookTime, instead of failing the limits check.
func TestZeroPowerSettingStartsAtTheMaximum(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cfg  appliance.MicrowaveConfig
		want uint8
	}{
		{"PWRNUM", appliance.MicrowaveConfig{Features: appliance.MicrowaveFeaturePowerAsNumber}, appliance.DefaultMaxPower},
		{"PWRNUM and PWRLMTS", appliance.MicrowaveConfig{
			Features: appliance.MicrowaveFeaturePowerAsNumber | appliance.MicrowaveFeaturePowerNumberLimits,
			MinPower: 20, MaxPower: 80, PowerStep: 20,
		}, 80},
	} {
		tc.cfg.Oven, tc.cfg.MaxCookTime = &oven{}, 600
		srv, err := appliance.NewMicrowaveOvenControl(tc.cfg)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if v, _ := srv.MatterRead(mwo.AttrPowerSetting); v != tc.want {
			t.Errorf("%s: PowerSetting %v, want %d", tc.name, v, tc.want)
		}
	}
}
