// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioning_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/commissioning"
)

// TestNewPASEResponderErrorDoesNotRenderThePasscode: the refusal of a
// passcode outside 1..99999998 names the rule and not the value (review
// II, R2-P0-sec-E04) — a nine-digit value is most often a real passcode
// with one digit too many.
func TestNewPASEResponderErrorDoesNotRenderThePasscode(t *testing.T) {
	t.Parallel()
	for _, passcode := range []uint32{99999999, 123456789, 202020211} {
		cfg := validPASEConfig()
		cfg.Passcode = passcode
		_, err := commissioning.NewPASEResponder(cfg)
		if !errors.Is(err, commissioning.ErrPASEInvalidPasscode) {
			t.Fatalf("passcode %d: err=%v, want ErrPASEInvalidPasscode", passcode, err)
		}
		if strings.Contains(err.Error(), strconv.FormatUint(uint64(passcode), 10)) {
			t.Errorf("the error text renders the passcode: %q", err)
		}
	}
}
