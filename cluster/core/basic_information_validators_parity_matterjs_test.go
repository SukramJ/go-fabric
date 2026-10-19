// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/basic-information/BasicInformationValidatorsTest.ts
// at 862ad75f: the VendorID/ProductID identity check of
// validateBasicInfoAttributes (basic-information-validators.ts:29-38).
//
// matter.js calls the validator directly; BasicInformationServer.ts:118 and
// BridgedDeviceBasicInformationServer.ts:45 call it from initialize. The Go
// validators run in the constructors (core.NewBasicInformation,
// core.NewBridgedDeviceBasicInformation), so a thrown ImplementationError is
// a constructor error here. The five identity cases are
// TestBasicInfo_ValidationVendorIDZero, …AboveFFF4, …ProductIDZero,
// …VendorID0001Accepted and …VendorIDFFF4Accepted (basic_information_test.go);
// this file ports the sixth.
package core_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestBasicInformationValidatorsSkipsAbsentIdentity ports "skips the check
// when VendorID/ProductID are absent (optional on bridged devices)"
// (BasicInformationValidatorsTest.ts:40-42): a state with uniqueId "abc"
// and serialNumber "def" and no vendorId/productId passes. On a Go bridged
// device an absent VendorID/ProductID is the zero value; the constructor
// accepts it and the two attributes are not served. NodeLabel is set
// because the Go constructor requires it.
func TestBasicInformationValidatorsSkipsAbsentIdentity(t *testing.T) {
	t.Parallel()
	b, err := core.NewBridgedDeviceBasicInformation(core.BridgedConfig{
		UniqueID: "abc", SerialNumber: "def", NodeLabel: "bridged", Reachable: true,
	})
	if err != nil {
		t.Fatalf("NewBridgedDeviceBasicInformation without VendorID/ProductID: %v, want no error", err)
	}
	attrs := b.MatterAttributes()
	for _, id := range []uint32{0x0002, 0x0004} { // VendorID, ProductID
		if slices.Contains(attrs, id) {
			t.Errorf("attribute 0x%04X served although absent", id)
		}
	}
	if v, ok := b.MatterRead(0x000F); !ok || v != "def" { // SerialNumber
		t.Errorf("SerialNumber = %v, %v; want def", v, ok)
	}
}
