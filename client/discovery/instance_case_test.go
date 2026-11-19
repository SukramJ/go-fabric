// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import "testing"

// TestInstanceIDMatchesWithoutCase: the instance id is a DNS label and is
// matched without regard to case, as the cache keys its names — ahead of
// matter.js, which compares it as received (CommissionableMdnsScanner.ts:535
// at b25ed960); connectedhomeip resolves the name and compares DNS names
// case-insensitively (src/lib/dnssd/wire/QName.cpp:141 at 6170af84)
// (review II, R2-P6-par-2152).
func TestInstanceIDMatchesWithoutCase(t *testing.T) {
	t.Parallel()
	dev := Device{InstanceID: "ABCD1234"}
	for _, id := range []string{"ABCD1234", "abcd1234", "AbCd1234"} {
		if !ByInstanceID(id).matches(&dev) {
			t.Errorf("ByInstanceID(%q) does not match the instance %q", id, dev.InstanceID)
		}
	}
	if ByInstanceID("ABCD1235").matches(&dev) {
		t.Error("another instance id matched")
	}
}
