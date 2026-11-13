// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package setup

import (
	"strconv"
	"strings"
	"testing"
)

// TestErrorsDoNotRenderThePasscode: an error of this package ends up in
// logs and in a controller's diagnostics, so none of them carries the
// passcode it refuses — it names the rule (review II, R2-P0-sec-E04).
// matter.js prints the value (packages/types/src/schema/PairingCodeSchema.ts:183
// at db021da0, `Invalid passcode ${passcode}`); the text is where this
// package departs from it.
func TestErrorsDoNotRenderThePasscode(t *testing.T) {
	t.Parallel()
	// Each passcode is refused by one of the rules: on the disallowed
	// list, above the range, or zero. The other fields are chosen so that
	// no digit run of the passcode appears in them.
	for _, passcode := range []uint32{12345678, 87654321, 99999999, 123456789, 0} {
		digits := strconv.FormatUint(uint64(passcode), 10)
		check := func(site string, err error) {
			t.Helper()
			if err == nil {
				t.Errorf("%s: passcode %d was accepted", site, passcode)
				return
			}
			// Zero is one digit, which any text may hold: there the text
			// must not print a value after the word.
			rendered := strings.Contains(err.Error(), digits)
			if passcode == 0 {
				rendered = strings.Contains(err.Error(), "asscode 0") || strings.Contains(err.Error(), "asscode=0")
			}
			if rendered {
				t.Errorf("%s: the error text renders the passcode: %q", site, err)
			}
		}
		p := Payload{Passcode: passcode, Discriminator: 3840, DiscoveryCaps: DiscoveryOnIP}
		check("Payload.Validate / QRCode", func() error { _, err := QRCode(p); return err }())
		_, err := ManualCode(3840, passcode)
		check("ManualCode", err)
		_, err = ManualCodeLong(3840, passcode, 0xFFF1, 0x8000)
		check("ManualCodeLong", err)
		if passcode > 99999998 || passcode == 0 {
			check("Payload.Validate", p.Validate())
		}
		// The decoders meet such a passcode in a code somebody else
		// wrote: encode without the check, then decode. The 27-bit and
		// the 8-digit fields cannot carry a value above the range.
		if passcode <= 99999998 {
			if passcode != 0 {
				qr, err := qrCode(p, false)
				if err != nil {
					t.Fatal(err)
				}
				_, err = DecodeQRCode(qr)
				check("DecodeQRCode", err)
			}
			manual, err := manualCode(3840, passcode, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeManualCode(manual)
			check("DecodeManualCode", err)
		}
	}
}
