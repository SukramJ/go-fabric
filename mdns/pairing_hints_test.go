// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"errors"
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/mdns"
)

// TestPairingHintsEveryBitAdvertised (R2-P5-apinode-03): each of the 23
// bits of [mdns.PairingHints] goes out in the `PH` key as set — bits 16 to
// 22, which the deprecated 16-bit PairingHint cannot carry, included — and
// the hints that need an instruction (bits 16 to 19 among them) are refused
// without one.
func TestPairingHintsEveryBitAdvertised(t *testing.T) {
	t.Parallel()
	needsInstruction := map[int]bool{4: true, 8: true, 9: true, 10: true, 11: true, 12: true, 15: true, 16: true, 17: true, 18: true, 19: true}
	if got := mdns.PairingHintPowerCycleUntilIndication; got != 1<<22 {
		t.Fatalf("the last hint is %#x, want bit 22", uint32(got))
	}
	for bit := range 23 {
		hint := mdns.PairingHints(1) << bit
		cfg := commissionableCfg()
		cfg.Hints = hint
		err := cfg.Validate()
		if needsInstruction[bit] != (err != nil) {
			t.Errorf("bit %d without an instruction: Validate() = %v, want refused %v", bit, err, needsInstruction[bit])
		}
		if needsInstruction[bit] {
			cfg.PairingInstruction = "3"
			if err := cfg.Validate(); err != nil {
				t.Errorf("bit %d with an instruction: %v", bit, err)
			}
		}
		if got := txtValue(mdns.BuildCommissionableService(cfg), "PH"); got != strconv.FormatUint(uint64(hint), 10) {
			t.Errorf("bit %d: PH=%q, want %d", bit, got, uint32(hint))
		}
	}
}

// TestPairingHintsAndDeprecatedHintExclusive: Hints and the deprecated
// PairingHint both set is refused; the deprecated field alone keeps its
// meaning, and neither sends the default.
func TestPairingHintsAndDeprecatedHintExclusive(t *testing.T) {
	t.Parallel()
	cfg := commissionableCfg()
	cfg.Hints = mdns.PairingHintPowerCycle
	cfg.PairingHint = 0x20
	if err := cfg.Validate(); !errors.Is(err, mdns.ErrInvalidService) {
		t.Errorf("both set: %v, want ErrInvalidService", err)
	}
	cfg.Hints = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("the deprecated hint alone: %v", err)
	}
	if got := txtValue(mdns.BuildCommissionableService(cfg), "PH"); got != "32" {
		t.Errorf("the deprecated hint alone: PH=%q, want 32", got)
	}
	cfg.PairingHint = 0
	if got := txtValue(mdns.BuildCommissionableService(cfg), "PH"); got != strconv.FormatUint(uint64(mdns.PairingHintsDefault), 10) || mdns.PairingHintsDefault != mdns.PairingHints(mdns.PairingHintDefault) {
		t.Errorf("no hint: PH=%q, want the default %d", got, uint32(mdns.PairingHintsDefault))
	}
}

// TestPairingHintsOutsideTheBitmapRefused: bits 23 to 31 of Hints name no
// hint and fail Validate, alone and beside a valid one; bits 20 to 22,
// outside matter.js's instruction set, take no PairingInstruction.
func TestPairingHintsOutsideTheBitmapRefused(t *testing.T) {
	t.Parallel()
	for bit := 23; bit < 32; bit++ {
		cfg := commissionableCfg()
		cfg.Hints = mdns.PairingHints(1)<<bit | mdns.PairingHintPowerCycle
		if err := cfg.Validate(); !errors.Is(err, mdns.ErrInvalidService) {
			t.Errorf("bit %d: Validate() = %v, want ErrInvalidService", bit, err)
		}
	}
	for _, hint := range []mdns.PairingHints{
		mdns.PairingHintPressAndReleaseSetupButtonNumberOfTimes,
		mdns.PairingHintPressButtonForNumberOfSecondsWithIndication,
		mdns.PairingHintPowerCycleUntilIndication,
	} {
		cfg := commissionableCfg()
		cfg.Hints, cfg.PairingInstruction = hint, "3"
		if err := cfg.Validate(); !errors.Is(err, mdns.ErrInvalidService) {
			t.Errorf("hint %#x with an instruction: Validate() = %v, want ErrInvalidService (not in the instruction set)", uint32(hint), err)
		}
	}
}
