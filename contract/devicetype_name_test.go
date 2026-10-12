// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestDeviceTypeNameNamesTheApplicationDeviceTypes pins the labels of the
// hand-kept table the generated one replaced (issue #57): none of the 31
// may change, so a host's grouping and search by label keep working.
func TestDeviceTypeNameNamesTheApplicationDeviceTypes(t *testing.T) {
	t.Parallel()
	cases := map[uint16]string{
		0x000A: "Door Lock",
		0x000F: "Generic Switch",
		0x0015: "Contact Sensor",
		0x0022: "Speaker",
		0x0027: "Mode Select",
		0x002B: "Fan",
		0x002C: "Air Quality Sensor",
		0x002D: "Air Purifier",
		0x0043: "Water Leak Detector",
		0x0073: "Laundry Washer",
		0x0074: "Robotic Vacuum Cleaner",
		0x0075: "Dishwasher",
		0x0076: "Smoke / CO Alarm",
		0x007A: "Extractor Hood",
		0x007C: "Laundry Dryer",
		0x0100: "On/Off Light",
		0x0101: "Dimmable Light",
		0x0106: "Light Sensor",
		0x0107: "Occupancy Sensor",
		0x010A: "On/Off Plug-in Unit",
		0x010C: "Color Temperature Light",
		0x010D: "Extended Color Light",
		0x0202: "Window Covering",
		0x0230: "Closure",
		0x0301: "Thermostat",
		0x0302: "Temperature Sensor",
		0x0303: "Pump",
		0x0305: "Pressure Sensor",
		0x0306: "Flow Sensor",
		0x0307: "Humidity Sensor",
		0x0510: "Electrical Sensor",
	}
	for id, want := range cases {
		if got := contract.DeviceTypeName(id); got != want {
			t.Errorf("DeviceTypeName(0x%04X) = %q, want %q", id, got, want)
		}
	}
	if got := contract.DeviceTypeName(0xFFF0); got != "0xFFF0" {
		t.Errorf("DeviceTypeName(unknown) = %q, want the hex fallback", got)
	}
	if got := contract.DeviceTypeName(0); got != "" {
		t.Errorf("DeviceTypeName(0) = %q, want empty", got)
	}
}

// TestDeviceTypeNameCoversEveryMeasurementClass: every device type a
// built-in measurement class advertises has a label, not the hex fallback
// — the shape of openccu-loom's
// TestW2PkgMatterDeviceTypeNameCoversEveryAdvertisedType, which caught
// SoilSensor (0x0045, MeasurementSoilMoisture) on the v0.3.0 bump (#57).
func TestDeviceTypeNameCoversEveryMeasurementClass(t *testing.T) {
	t.Parallel()
	for class := contract.MeasurementNone + 1; class <= contract.MeasurementSoilMoisture; class++ {
		dt := contract.MeasurementClassDeviceType(class)
		if dt == 0 {
			continue
		}
		if got := contract.DeviceTypeName(dt); got == "" || got == fmt.Sprintf("0x%04X", dt) {
			t.Errorf("measurement class %d advertises 0x%04X, which has no label (%q)", class, dt, got)
		}
	}
	if got := contract.DeviceTypeName(0x0045); got != "SoilSensor" {
		t.Errorf("DeviceTypeName(0x0045) = %q, want SoilSensor", got)
	}
}
