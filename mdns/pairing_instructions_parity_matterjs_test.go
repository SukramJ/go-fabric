// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestPairingInstructionsParity holds
// [mdns.CommissionableServiceConfig.Validate] against matter.js
// #validatePairingInstructions
// (packages/protocol/src/advertisement/mdns/CommissionableMdnsAdvertisement.ts
// :106-135 at db021da0), one case per throw and the accepted shapes
// between them. The bits are PairingHintBitmap's
// (packages/protocol/src/advertisement/PairingHintBitmap.ts at db021da0):
// powerCycle 0, customInstruction 4, deviceManual 5, pressResetButton 6,
// pressResetButtonForNumberOfSeconds 8, pressSetupButtonForNumberOfSeconds
// 15. The throw for a hint object with every flag false (:110-112) has no
// form in Go, where PairingHint 0 means "no hint given".
func TestPairingInstructionsParity(t *testing.T) {
	t.Parallel()
	const (
		powerCycle          uint16 = 1 << 0
		customInstruction   uint16 = 1 << 4
		deviceManual        uint16 = 1 << 5
		pressResetButton    uint16 = 1 << 6
		pressResetForNSecs  uint16 = 1 << 8
		pressSetupForNSecs  uint16 = 1 << 15
		noInstructionNeeded        = powerCycle | deviceManual | pressResetButton
	)
	for _, tc := range []struct {
		name        string
		hint        uint16
		instruction string
		wantErr     string // empty: accepted
	}{
		// :130-132 — `else if (pairingInstructions)`: no hint at all.
		{"instruction without a hint", 0, "5", "pairing instructions provided but no pairing hint set"},
		{"neither", 0, "", ""},
		// :118-121 — more than one hint that requires an instruction.
		{"two hints requiring instructions", customInstruction | pressResetForNSecs, "5", "only one pairing hint requiring instructions can be set, but got 2"},
		{"two hints requiring instructions, the top bit one of them", pressSetupForNSecs | pressResetForNSecs, "5", "only one pairing hint requiring instructions can be set, but got 2"},
		// :122-125 — one such hint and no instruction.
		{"hint requiring instructions without one", powerCycle | pressResetForNSecs, "", "pairing instructions are required when a pairing hint requiring instructions is set"},
		// :126-129 — an instruction and no hint that requires one.
		{"instruction without a hint requiring one", noInstructionNeeded, "5", "pairing instructions provided but no pairing hint requiring instructions set"},
		{"one hint requiring instructions, with it", powerCycle | pressResetForNSecs, "5", ""},
		{"custom instruction, with it", customInstruction, "Hold the button", ""},
		{"hints that need no instruction", noInstructionNeeded, "", ""},
		{"the default hint", mdns.PairingHintDefault, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := commissionableCfg()
			cfg.PairingHint, cfg.PairingInstruction = tc.hint, tc.instruction
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, mdns.ErrInvalidService) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate = %v, want ErrInvalidService with %q", err, tc.wantErr)
			}
		})
	}
}
