// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"strings"
	"testing"

	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
)

// TestBridgedInfoBeyondItsConstraintIsRefused (R2-P6-wire-2502, decision
// 201): a declared BridgedDeviceBasicInformation value beyond the
// definition's constraint is refused by New with ErrConfiguration, naming
// the attribute — never served over-long; a value at the maximum is
// served.
func TestBridgedInfoBeyondItsConstraintIsRefused(t *testing.T) {
	t.Parallel()
	s := func(n int) *string { v := strings.Repeat("x", n); return &v }
	for name, p := range map[string]bdbidef.Patch{
		"ProductName":       {ProductName: s(33)},
		"ProductUrl":        {ProductUrl: s(300)},
		"SerialNumber":      {SerialNumber: s(100)},
		"ManufacturingDate": {ManufacturingDate: s(17)},
		"VendorName":        {VendorName: s(33)},
	} {
		_, err := NewOnOffLight(testOptions("bdbi-"+name), bdbidef.Declare{Initial: p})
		if !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s beyond its constraint: %v, want ErrConfiguration naming %s", name, err, name)
		}
	}
	d, err := NewOnOffLight(testOptions("bdbi-max"), bdbidef.Declare{Initial: bdbidef.Patch{ProductName: s(32), VendorName: s(32)}})
	if err != nil {
		t.Fatalf("ProductName and VendorName of 32 bytes: %v", err)
	}
	if got := d.Spec().BridgedBasicInformation.ProductName; got == nil || len(*got) != 32 {
		t.Errorf("ProductName served %v, want the 32 bytes", got)
	}

	opts := testOptions("vendor")
	opts.Vendor = *s(33)
	if _, err := NewOnOffLight(opts); !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), "VendorName") {
		t.Errorf("Options.Vendor beyond VendorName's constraint: %v, want ErrConfiguration naming VendorName", err)
	}
}
