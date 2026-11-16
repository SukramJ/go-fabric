// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"bytes"
	"testing"
)

// TestRegistrationsAllDoesNotAliasKeys holds R2-P6-api-0226: a key in the
// slice All returns is the caller's copy — writing to it does not change
// the table, as Set does not keep the caller's slice either.
func TestRegistrationsAllDoesNotAliasKeys(t *testing.T) {
	t.Parallel()
	var r Registrations
	r.Set(Registration{CheckInNodeID: 7, Key: bytes.Repeat([]byte{0xAA}, 16)})
	got := r.All()
	got[0].Key[0] = 0x00
	if again := r.All(); again[0].Key[0] != 0xAA {
		t.Fatalf("a write to a key All returned changed the table: key[0] = %#x, want 0xaa", again[0].Key[0])
	}
}
