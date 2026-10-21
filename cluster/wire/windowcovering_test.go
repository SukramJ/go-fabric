// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestDecodeGoToLiftValueTag0 verifies that context tag 0 carries the
// raw uint16 position (1234 → LiftValue=1234).
func TestDecodeGoToLiftValueTag0(t *testing.T) {
	t.Parallel()
	payload := buildPayload(t, func(e *tlv.Encoder) {
		e.PutUint(tlv.ContextTag(0), 1234)
	})
	got, err := wire.DecodeGoToLiftValue(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LiftValue != 1234 {
		t.Errorf("LiftValue = %d, want 1234", got.LiftValue)
	}
}

// TestDecodeGoToLiftValueMalformed verifies that a truncated payload
// wraps ErrWindowCoveringMalformed.
func TestDecodeGoToLiftValueMalformed(t *testing.T) {
	t.Parallel()
	_, err := wire.DecodeGoToLiftValue([]byte{0x15})
	if !errors.Is(err, wire.ErrWindowCoveringMalformed) {
		t.Fatalf("err = %v, want ErrWindowCoveringMalformed", err)
	}
}

// TestDecodeGoToTiltValueTag0 verifies that context tag 0 carries the
// raw uint16 position (4321 → TiltValue=4321).
func TestDecodeGoToTiltValueTag0(t *testing.T) {
	t.Parallel()
	payload := buildPayload(t, func(e *tlv.Encoder) {
		e.PutUint(tlv.ContextTag(0), 4321)
	})
	got, err := wire.DecodeGoToTiltValue(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TiltValue != 4321 {
		t.Errorf("TiltValue = %d, want 4321", got.TiltValue)
	}
}

// TestDecodeGoToTiltValueMalformed verifies that a truncated payload
// wraps ErrWindowCoveringMalformed.
func TestDecodeGoToTiltValueMalformed(t *testing.T) {
	t.Parallel()
	_, err := wire.DecodeGoToTiltValue([]byte{0x15})
	if !errors.Is(err, wire.ErrWindowCoveringMalformed) {
		t.Fatalf("err = %v, want ErrWindowCoveringMalformed", err)
	}
}
