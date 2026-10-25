// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPakePasscodeVerifier: the verifier is w0 ‖ L, 32 + 65 bytes, and
// the device side's NewVerifierFromValue reads back the same w0 and L
// NewVerifierContext derives.
func TestPakePasscodeVerifier(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	v, err := pakePasscodeVerifier(20202021, salt, windowIterations)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != spake2.VerifierW0Size+spake2.VerifierLSize {
		t.Fatalf("verifier length %d", len(v))
	}
	got, err := spake2.NewVerifierFromValue(v[:spake2.VerifierW0Size], v[spake2.VerifierW0Size:])
	if err != nil {
		t.Fatal(err)
	}
	want, err := spake2.NewVerifierContext(20202021, salt, windowIterations)
	if err != nil {
		t.Fatal(err)
	}
	if got.W0.Cmp(want.W0) != 0 || !got.L.Equal(want.L) {
		t.Fatal("verifier does not round-trip")
	}
}

// TestRandomWindowCredentials: every passcode is a valid setup passcode
// and every discriminator 12 bits.
func TestRandomWindowCredentials(t *testing.T) {
	for range 1000 {
		p, err := randomPasscode()
		if err != nil || !setup.IsValidPasscode(p) {
			t.Fatalf("passcode %d %v", p, err)
		}
		d, err := randomDiscriminator()
		if err != nil || d > setup.MaxDiscriminator {
			t.Fatalf("discriminator %d %v", d, err)
		}
	}
}
