// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// White-box tests for unexported helpers in general_diagnostics.go and
// basic_information.go. Uses package core (not core_test) to access
// unexported functions.

package core

import (
	"testing"
)

// TestNetworkInterfaceRules: every entry carries the root's network type,
// Ethernet by default, whatever its name; without an advertiser
// restriction every interface is operational, with one the advertised
// ones alone (GeneralDiagnosticsServer.ts:477-503 at db021da0).
func TestNetworkInterfaceRules(t *testing.T) {
	t.Parallel()
	var zero networkInterfaceRules
	for _, name := range []string{"eth0", "wlan0", "br0", "bond0", "awdl0"} {
		if got := zero.typeOf(); got != InterfaceTypeEthernet {
			t.Errorf("%s: default type %d, want Ethernet", name, got)
		}
		if !zero.operational(name) {
			t.Errorf("%s: not operational without a restriction", name)
		}
	}
	wifi := networkInterfaceRules{interfaceType: InterfaceTypeWiFi, advertised: []string{"br0", "wlan0"}}
	if got := wifi.typeOf(); got != InterfaceTypeWiFi {
		t.Errorf("type %d, want WiFi", got)
	}
	for name, want := range map[string]bool{"br0": true, "wlan0": true, "eth0": false} {
		if got := wifi.operational(name); got != want {
			t.Errorf("%s: operational %v, want %v", name, got, want)
		}
	}
}

// ─── basicInfoSerialFromUniqueID ─────────────────────────────────────────────

// TestBasicInfoSerialFromUniqueID_Short verifies that a uid ≤ 16 chars is
// returned as-is (the `return uid` branch at line 425-427 of basic_information.go).
func TestBasicInfoSerialFromUniqueID_Short(t *testing.T) {
	t.Parallel()
	uid := "0123456789abcdef" // exactly 16 chars
	got := basicInfoSerialFromUniqueID(uid)
	if got != uid {
		t.Errorf("basicInfoSerialFromUniqueID(%q) = %q, want %q", uid, got, uid)
	}
}

// TestBasicInfoSerialFromUniqueID_Long verifies that a uid > 16 chars is
// truncated to the first 16 characters.
func TestBasicInfoSerialFromUniqueID_Long(t *testing.T) {
	t.Parallel()
	uid := "0123456789abcdef0011223344556677" // 32 chars
	got := basicInfoSerialFromUniqueID(uid)
	if got != uid[:16] {
		t.Errorf("basicInfoSerialFromUniqueID(%q) = %q, want %q", uid, got, uid[:16])
	}
}
