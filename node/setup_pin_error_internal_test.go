// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// TestSetupPINErrorsDoNotRenderThePasscode: no passcode check of this
// package renders the passcode it refuses — the error names the rule
// (review II, R2-P0-sec-E04; the encoders' half is
// secure/setup TestErrorsDoNotRenderThePasscode). An error text is logged
// by the host and a passcode on the disallowed list is still the passcode
// of the device that was given it.
func TestSetupPINErrorsDoNotRenderThePasscode(t *testing.T) {
	t.Parallel()
	for _, pin := range []uint32{11111111, 12345678, 87654321, 99999999, 123456789} {
		digits := strconv.FormatUint(uint64(pin), 10)
		check := func(site string, err error) {
			t.Helper()
			if err == nil {
				t.Errorf("%s: passcode %d was accepted", site, pin)
				return
			}
			if strings.Contains(err.Error(), digits) {
				t.Errorf("%s: the error text renders the passcode: %q", site, err)
			}
			if !strings.Contains(err.Error(), "passcode") {
				t.Errorf("%s: the error text does not name the passcode check: %q", site, err)
			}
		}

		err := ValidateSetupPIN(pin)
		check("ValidateSetupPIN", err)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("ValidateSetupPIN(%d) = %v, want ErrInvalidConfig", pin, err)
		}

		cfg := Config{Commissioning: Commissioning{Passcode: pin}}
		check("Config.Validate", cfg.Validate())

		creds := WindowCredentials{Passcode: pin}
		check("WindowCredentials.validate", creds.validate())

		_, err = buildPaseAdapter(nil, nil, paseParams{passcode: pin}, nil)
		check("buildPaseAdapter", err)
	}
}
