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
// Ethernet by default, whatever its name — matter.js's fallback for an
// interface the platform gives no type (GeneralDiagnosticsServer.ts:527,
// :477-494 at db021da0), which every entry takes here, the module having
// no platform mapping; without an advertiser restriction every interface
// is operational, with one the advertised ones alone (:496-503).
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
