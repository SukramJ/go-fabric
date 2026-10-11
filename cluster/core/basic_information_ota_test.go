// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestBasicInfo_OtaNodeInformation: what the OTA requestor reads
// (OtaSoftwareUpdateRequestorServer.ts #basicInformationState), and the
// version an applied update sets.
func TestBasicInfo_OtaNodeInformation(t *testing.T) {
	t.Parallel()
	bi, err := core.NewBasicInformation(core.Config{
		VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "n", HardwareVersion: 3,
		SoftwareVersion: 1, SoftwareVersionStr: "1", Location: "DE",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := bi.OtaNodeInformation()
	want := core.OtaNodeInformation{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, SoftwareVersionString: "1", HardwareVersion: 3, Location: "DE"}
	if got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
	before := bi.MatterDataVersion()
	if err := bi.SetSoftwareVersion(2, "2.0"); err != nil {
		t.Fatal(err)
	}
	if v, _ := bi.MatterRead(0x0009); v != uint32(2) {
		t.Fatalf("SoftwareVersion %v", v)
	}
	if v, _ := bi.MatterRead(0x000A); v.(tlv.BoundedString).Value != "2.0" { //nolint:forcetypeassert // BasicInformation answers a BoundedString
		t.Fatalf("SoftwareVersionString %v", v)
	}
	if bi.MatterDataVersion() == before {
		t.Fatal("data version not bumped")
	}
	if err := bi.SetSoftwareVersion(3, ""); err == nil {
		t.Fatal("empty version string accepted")
	}
}
