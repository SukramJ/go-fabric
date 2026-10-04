// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestDeviceTypeNameNamesTheApplicationDeviceTypes pins the labels of the
// device types the application cluster servers exist for, so none of
// them reaches the hex fallback an operator cannot search by name.
func TestDeviceTypeNameNamesTheApplicationDeviceTypes(t *testing.T) {
	t.Parallel()
	cases := map[uint16]string{
		0x002B: "Fan",
		0x002D: "Air Purifier",
		0x0076: "Smoke / CO Alarm",
		0x007A: "Extractor Hood",
		0x0306: "Flow Sensor",
	}
	for id, want := range cases {
		if got := contract.DeviceTypeName(id); got != want {
			t.Errorf("DeviceTypeName(0x%04X) = %q, want %q", id, got, want)
		}
	}
	if got := contract.DeviceTypeName(0xFFF0); got != "0xFFF0" {
		t.Errorf("DeviceTypeName(unknown) = %q, want the hex fallback", got)
	}
}
