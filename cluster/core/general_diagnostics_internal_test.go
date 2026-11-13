// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// White-box tests for unexported helpers in general_diagnostics.go and
// basic_information.go. Uses package core (not core_test) to access
// unexported functions.

package core

import (
	"testing"
)

// TestClassifyInterface_AllBranches exercises every named case in the
// classifyInterface heuristic.
func TestClassifyInterface_AllBranches(t *testing.T) {
	t.Parallel()

	type tc struct {
		name string
		want uint8
	}
	cases := []tc{
		// loopback
		{"lo", InterfaceTypeUnspecified},
		{"lo0", InterfaceTypeUnspecified},
		// WiFi
		{"wlan0", InterfaceTypeWiFi},
		{"wlo1", InterfaceTypeWiFi},
		{"wifi0", InterfaceTypeWiFi},
		// Thread
		{"thread0", InterfaceTypeThread},
		{"trel0", InterfaceTypeThread},
		// Ethernet
		{"eth0", InterfaceTypeEthernet},
		{"en0", InterfaceTypeEthernet},
		{"en1", InterfaceTypeEthernet},
		// Unknown → Unspecified
		{"docker0", InterfaceTypeUnspecified},
		{"vpn0", InterfaceTypeUnspecified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyInterface(tc.name)
			if got != tc.want {
				t.Errorf("classifyInterface(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
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
