// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package setup

import (
	"errors"
	"testing"
)

// TestQRCode_RefusesWhatMatterJSRefuses: QrPairingCodeSchema.validate
// (packages/types/src/schema/PairingCodeSchema.ts:279-286 at 0ceb202d)
// refuses a version other than 0 and the INVALID_PASSCODES (:160-162).
// The vendor/product identity is a decode-side rule (:288-293), so an
// encoder accepts a vendor without a product.
func TestQRCode_RefusesWhatMatterJSRefuses(t *testing.T) {
	t.Parallel()
	base := Payload{VendorID: 0xFFF1, ProductID: 0x8000, Discriminator: 3840, Passcode: 20202021, DiscoveryCaps: DiscoveryOnIP}
	if _, err := QRCode(base); err != nil {
		t.Fatalf("a valid payload: %v", err)
	}
	v1 := base
	v1.Version = 1
	if _, err := QRCode(v1); err == nil {
		t.Error("version 1 encoded")
	}
	for _, pc := range []uint32{11111111, 12345678, 87654321, 99999999} {
		p := base
		p.Passcode = pc
		if _, err := QRCode(p); err == nil {
			t.Errorf("passcode %d encoded", pc)
		}
	}
	anon := base
	anon.ProductID = 0
	if _, err := QRCode(anon); err != nil {
		t.Errorf("the encoder applies the decode-side identity rule: %v", err)
	}
}

// TestManualCode_RefusesTrivialPasscode: ManualPairingCodeSchema.validate
// runs assertValidPasscode (PairingCodeSchema.ts:429-430 at 0ceb202d). The
// layout itself is unchanged: the unchecked encoder still writes the
// vector the code had before.
func TestManualCode_RefusesTrivialPasscode(t *testing.T) {
	t.Parallel()
	if _, err := ManualCode(0x123, 12345678); err == nil {
		t.Fatal("ManualCode(0x123, 12345678) encoded a trivial passcode")
	}
	if got, err := manualCode(0x123, 12345678, nil, false); err != nil || got != "02491007533" {
		t.Fatalf("unchecked layout %q, %v", got, err)
	}
}

// TestManualCodeLong: the 21-digit form of ManualPairingCodeSchema.encodeInternal
// (PairingCodeSchema.ts:440-454 at 0ceb202d) — VID_PID_PRESENT in the first
// digit, five digits each of vendor and product, then the Verhoeff digit —
// read back by DecodeManualCode.
func TestManualCodeLong(t *testing.T) {
	t.Parallel()
	code, err := ManualCodeLong(0xF00, 20202021, 0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	short, _ := ManualCode(0xF00, 20202021)
	if len(code) != 21 || code[0] != '7' || code[1:10] != short[1:10] || code[10:20] != "6552132768" {
		t.Fatalf("ManualCodeLong = %q (short %q)", code, short)
	}
	got, err := DecodeManualCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasVendorProduct || got.VendorID != 0xFFF1 || got.ProductID != 0x8000 || got.Passcode != 20202021 || got.ShortDiscriminator != 0xF {
		t.Fatalf("decoded %+v", got)
	}
	if _, err := ManualCodeLong(0xF00, 11111111, 0xFFF1, 0x8000); err == nil {
		t.Fatal("a trivial passcode encoded")
	}
	if _, err := ManualCodeLong(0x1000, 20202021, 0xFFF1, 0x8000); err == nil || errors.Is(err, nil) {
		t.Fatal("a 13-bit discriminator encoded")
	}
}
