// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package setup

import (
	"strings"
	"testing"
)

// This package carries no parser: its exported surface is [QRCode],
// [ManualCode], [Payload.Validate] and [IsValidSetupPIN], all of which
// encode or classify. There is no base38 or manual-code decoder to feed
// attacker-supplied bytes into, so there is no decode(encode(x)) round-trip
// to assert either. What is fuzzable is the other direction — the field
// values an operator or a config file can set, which are equally
// untrusted at the point they reach the encoders, and which are the
// inputs the bit-packing can overflow on.
//
// The seeds are the vectors the package's own tests already pin
// (standardPayload, and the ManualCode known-vector inputs), plus
// out-of-range values that must be rejected rather than silently
// truncated into a valid-looking code.

// FuzzQRCode drives [QRCode] over the whole payload field space.
//
// Asserted: the call terminates without panicking, and every string it
// returns is a well-formed onboarding payload — the "MT:" prefix, the
// 22-character total length that an 11-byte packed payload always
// produces, and the base38 alphabet. Those three are the properties
// TestQRCode_StartsWithMT / _ExactLength / _OnlyBase38Alphabet pin for
// one vector; here they must hold for every accepted payload. Rejected
// payloads assert nothing beyond "an error came back".
func FuzzQRCode(f *testing.F) {
	add := func(p Payload) {
		f.Add(p.Version, p.VendorID, p.ProductID, p.CustomFlow, uint8(p.DiscoveryCaps), p.Discriminator, p.Passcode)
	}
	add(standardPayload)
	add(Payload{DiscoveryCaps: DiscoveryOnIP, Discriminator: 0, Passcode: 1})
	add(Payload{DiscoveryCaps: DiscoveryBLE | DiscoverySoftAP, Discriminator: MaxDiscriminator, Passcode: 99999998})
	// Deliberately malformed: every field one step past its bit width.
	add(Payload{Version: 0xFF, CustomFlow: 0xFF, Discriminator: MaxDiscriminator + 1, Passcode: 99999999})
	f.Add(uint8(0), uint16(0), uint16(0), uint8(0), uint8(0), uint16(0), uint32(0)) // passcode 0 — rejected

	f.Fuzz(func(t *testing.T, version uint8, vendorID, productID uint16, customFlow, caps uint8, discriminator uint16, passcode uint32) {
		p := Payload{
			Version:       version,
			VendorID:      vendorID,
			ProductID:     productID,
			CustomFlow:    customFlow,
			DiscoveryCaps: DiscoveryCaps(caps),
			Discriminator: discriminator,
			Passcode:      passcode,
		}
		qr, err := QRCode(p)
		if err != nil {
			if qr != "" {
				t.Fatalf("QRCode returned %q alongside error %v", qr, err)
			}
			return
		}
		if !strings.HasPrefix(qr, "MT:") {
			t.Fatalf("QRCode(%+v) = %q, want an \"MT:\" prefix", p, qr)
		}
		// 11 packed bytes → 3 groups of 3 (5 chars each) + 1 group of 2
		// (4 chars) = 19, plus the prefix.
		if len(qr) != 22 {
			t.Fatalf("QRCode(%+v) = %q (len=%d), want 22 characters", p, qr, len(qr))
		}
		for i, ch := range strings.TrimPrefix(qr, "MT:") {
			if !strings.ContainsRune(base38Alphabet, ch) {
				t.Fatalf("QRCode(%+v) = %q: character %d (%q) is outside the base38 alphabet", p, qr, i, ch)
			}
		}
	})
}

// FuzzManualCode drives [ManualCode] over the full discriminator ×
// passcode space.
//
// Asserted: no panic, and every accepted pair yields a code a
// commissioner can actually key in — 11 decimal digits whose 11th is the
// Verhoeff check digit over the first ten. The check digit is recomputed
// with the package's own [verhoeffCheck] rather than a second
// implementation, so the assertion covers the digit LAYOUT (a packing
// overflow shifts the first ten digits and the frozen check digit no
// longer matches) without re-deriving the Verhoeff tables.
func FuzzManualCode(f *testing.F) {
	// The inputs behind TestManualCode_KnownVectors' frozen outputs.
	f.Add(uint16(0xF00), uint32(20202021))
	f.Add(uint16(0x123), uint32(12345678))
	f.Add(uint16(0xFFF), uint32(99999998))
	f.Add(uint16(0), uint32(1))
	// Deliberately out of range on each axis.
	f.Add(uint16(0x1000), uint32(20202021))
	f.Add(uint16(0xF00), uint32(0))
	f.Add(uint16(0xFFFF), uint32(0xFFFFFFFF))

	f.Fuzz(func(t *testing.T, discriminator uint16, passcode uint32) {
		mc, err := ManualCode(discriminator, passcode)
		if err != nil {
			if mc != "" {
				t.Fatalf("ManualCode returned %q alongside error %v", mc, err)
			}
			return
		}
		if len(mc) != 11 {
			t.Fatalf("ManualCode(%#x, %d) = %q (len=%d), want 11 digits", discriminator, passcode, mc, len(mc))
		}
		for i := range len(mc) {
			if mc[i] < '0' || mc[i] > '9' {
				t.Fatalf("ManualCode(%#x, %d) = %q: byte %d is not a decimal digit", discriminator, passcode, mc, i)
			}
		}
		want, err := verhoeffCheck(mc[:10])
		if err != nil {
			t.Fatalf("verhoeffCheck over %q: %v", mc[:10], err)
		}
		if got := int(mc[10] - '0'); got != want {
			t.Fatalf("ManualCode(%#x, %d) = %q: check digit %d, want %d",
				discriminator, passcode, mc, got, want)
		}
	})
}

// FuzzIsValidSetupPIN pins the classifier against the range rule it
// shares with [Payload.Validate]: the two disagree only over the trivial
// passcode blacklist, which Validate does not apply. A passcode
// IsValidSetupPIN accepts must therefore always pass Validate's range
// check, and never the other way round.
func FuzzIsValidSetupPIN(f *testing.F) {
	f.Add(uint32(20202021))
	f.Add(uint32(0))
	f.Add(uint32(12345678)) // trivial — rejected by IsValidSetupPIN only
	f.Add(uint32(99999998))
	f.Add(uint32(99999999)) // out of range

	f.Fuzz(func(t *testing.T, passcode uint32) {
		if !IsValidSetupPIN(passcode) {
			return
		}
		p := standardPayload
		p.Passcode = passcode
		if err := p.Validate(); err != nil {
			t.Fatalf("IsValidSetupPIN(%d) accepts a passcode Payload.Validate rejects: %v", passcode, err)
		}
	})
}
