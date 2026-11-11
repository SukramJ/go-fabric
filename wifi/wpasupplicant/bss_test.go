// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"testing"

	"github.com/godbus/dbus/v5"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
)

// TestBandAndChannel: connectedhomeip's frequency table, branch by
// branch.
func TestBandAndChannel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		freq    uint32
		band    netcommdef.WiFiBandEnum
		channel uint16
	}{
		{0, netcommdef.WiFiBandV1G, 1},
		{863, netcommdef.WiFiBandV1G, 0},
		{902, netcommdef.WiFiBandV1G, 0},
		{910, netcommdef.WiFiBandV1G, 16},
		{916, netcommdef.WiFiBandV1G, 0xFFFF}, // chip's unsigned wrap
		{920, netcommdef.WiFiBandV1G, 7},
		{2412, netcommdef.WiFiBandV2G4, 1},
		{2472, netcommdef.WiFiBandV2G4, 13},
		{2484, netcommdef.WiFiBandV2G4, 14},
		{2478, netcommdef.WiFiBandV2G4, 0},
		{3650, netcommdef.WiFiBandV3G65, 0},
		{5035, netcommdef.WiFiBandV5G, 7},
		{5180, netcommdef.WiFiBandV5G, 36},
		{5945, netcommdef.WiFiBandV5G, 189},
		{5960, netcommdef.WiFiBandV5G, 192},
		{5980, netcommdef.WiFiBandV5G, 196},
		{5955, netcommdef.WiFiBandV6G, 1},
		{6115, netcommdef.WiFiBandV6G, 33},
		{4000, netcommdef.WiFiBandV2G4, 0},
		{60480, netcommdef.WiFiBandV6G, 10906}, // the 6 GHz branch shadows 60 GHz, as in chip
	} {
		band, channel := bandAndChannel(tc.freq)
		if band != tc.band || channel != tc.channel {
			t.Errorf("bandAndChannel(%d) = %d, %d; want %d, %d", tc.freq, band, channel, tc.band, tc.channel)
		}
	}
}

// TestSecurity: chip's key-management mapping, case-insensitive.
func TestSecurity(t *testing.T) {
	t.Parallel()
	km := func(list ...string) map[string]dbus.Variant {
		return map[string]dbus.Variant{"KeyMgmt": dbus.MakeVariant(list)}
	}
	for _, tc := range []struct {
		name     string
		wpa, rsn map[string]dbus.Variant
		want     netcommdef.WiFiSecurityBitmap
	}{
		{"none", nil, nil, netcommdef.WiFiSecurityUnencrypted},
		{"empty lists", km(), km(), netcommdef.WiFiSecurityUnencrypted},
		{"wpa-psk", km("wpa-psk"), nil, netcommdef.WiFiSecurityWpaPersonal},
		{"wpa-none", km("WPA-NONE"), nil, netcommdef.WiFiSecurityWpaPersonal},
		{"rsn psk", nil, km("wpa-psk"), netcommdef.WiFiSecurityWpa2Personal},
		{"rsn psk-sha256", nil, km("wpa-psk-sha256"), netcommdef.WiFiSecurityWpa2Personal},
		{"rsn ft-psk", nil, km("wpa-ft-psk"), netcommdef.WiFiSecurityWpa2Personal},
		{"sae", nil, km("SAE"), netcommdef.WiFiSecurityWpa3Personal},
		{"transition", km("wpa-psk"), km("wpa-psk", "sae"), netcommdef.WiFiSecurityWpaPersonal | netcommdef.WiFiSecurityWpa2Personal | netcommdef.WiFiSecurityWpa3Personal},
		{"wpa eap only", km("wpa-eap"), nil, 0},
		{"rsn eap only", nil, km("wpa-eap-sha256", "wpa-ft-eap"), 0},
		{"eap and psk", nil, km("wpa-eap", "wpa-psk"), netcommdef.WiFiSecurityWpa2Personal},
		{"unknown", nil, km("owe"), netcommdef.WiFiSecurityUnencrypted},
	} {
		if got := security(tc.wpa, tc.rsn); got != tc.want {
			t.Errorf("%s: security = %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

func TestClampSignal(t *testing.T) {
	t.Parallel()
	for in, want := range map[int16]int8{-129: -128, -128: -128, 0: 0, 127: 127, 128: 127, -40: -40} {
		if got := clampSignal(in); got != want {
			t.Errorf("clampSignal(%d) = %d", in, got)
		}
	}
}
