// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
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

// TestSetSoftwareVersionReportsWhatChanged (N-flake-1): a host's
// SetSoftwareVersion reports SoftwareVersion and SoftwareVersionString to
// the change listeners with the host's origin, each only when it differs,
// and advances the version once; the values held report nothing and leave
// the version. Mirrors matter.js ServerBehaviorBacking.ts:163-168 and
// Datasource.ts:1456-1457, :1476-1481 at db021da0.
func TestSetSoftwareVersionReportsWhatChanged(t *testing.T) {
	t.Parallel()
	bi, err := core.NewBasicInformation(core.Config{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, SoftwareVersionStr: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var heard []contract.Change
	bi.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		heard = append(heard, c)
		mu.Unlock()
	})
	got := func() []contract.Change {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(heard)
	}
	before := bi.MatterDataVersion()
	if err := bi.SetSoftwareVersion(2, "2.0"); err != nil {
		t.Fatal(err)
	}
	h := got()
	if len(h) != 1 || !slices.Equal(h[0].Attributes, []uint32{0x0009, 0x000A}) || h[0].Origin != contract.OriginHostSet {
		t.Fatalf("reported %+v, want SoftwareVersion and SoftwareVersionString once, from the host", h)
	}
	if bi.MatterDataVersion() != before+1 {
		t.Errorf("version %d → %d, want one advance", before, bi.MatterDataVersion())
	}
	after := bi.MatterDataVersion()
	if err := bi.SetSoftwareVersion(2, "2.0"); err != nil {
		t.Fatal(err)
	}
	if len(got()) != 1 || bi.MatterDataVersion() != after {
		t.Errorf("the values held reported %+v and moved the version %d → %d", got()[1:], after, bi.MatterDataVersion())
	}
	if err := bi.SetSoftwareVersion(2, "2.0.1"); err != nil {
		t.Fatal(err)
	}
	if last := got(); len(last) != 2 || !slices.Equal(last[1].Attributes, []uint32{0x000A}) {
		t.Errorf("a string-only change reported %+v, want SoftwareVersionString alone", last[1:])
	}
}
