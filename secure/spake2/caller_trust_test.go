// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"errors"
	"math/big"
	"testing"
)

// TestPake2OutputYIsTheCallersCopy holds R2-P6-api-0115 (a): the Y a
// Pake2Output carries is a copy. A caller that changes it after
// ProcessPake1 changes nothing the verifier checks cA against, so the
// handshake still completes with the prover's genuine cA.
func TestPake2OutputYIsTheCallersCopy(t *testing.T) {
	t.Parallel()
	salt := testSalt()
	vc, err := NewVerifierContext(testPasscode, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	prover, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier(vc, nil, nil, nil)
	pake2, err := verifier.ProcessPake1(pA)
	if err != nil {
		t.Fatal(err)
	}
	y := append([]byte(nil), pake2.Y...)
	cA, err := prover.ProcessPake2(y, pake2.CB)
	if err != nil {
		t.Fatal(err)
	}
	for i := range pake2.Y {
		pake2.Y[i] ^= 0xFF
	}
	if err := verifier.ProcessPake3(cA); err != nil {
		t.Fatalf("ProcessPake3 after the caller changed its Y: %v, want the genuine cA accepted", err)
	}
}

// TestGeneratePake1XIsTheCallersCopy: the X GeneratePake1 returns is a
// copy, as Pake2Output's Y is. A caller that changes it after sending it
// changes nothing the prover checks cB against or derives its keys from,
// so the handshake still completes (review II, R2-P7-sec-06).
func TestGeneratePake1XIsTheCallersCopy(t *testing.T) {
	t.Parallel()
	salt := testSalt()
	vc, err := NewVerifierContext(testPasscode, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	prover, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewVerifier(vc, nil, nil, nil)
	pake2, err := verifier.ProcessPake1(append([]byte(nil), pA...))
	if err != nil {
		t.Fatal(err)
	}
	for i := range pA {
		pA[i] ^= 0xFF
	}
	cA, err := prover.ProcessPake2(pake2.Y, pake2.CB)
	if err != nil {
		t.Fatalf("ProcessPake2 after the caller changed its X: %v, want the verifier's genuine cB accepted", err)
	}
	if err := verifier.ProcessPake3(cA); err != nil {
		t.Fatalf("ProcessPake3: %v", err)
	}
}

// TestProcessPake1RefusesAVerifierContextThatDoesNotFit holds
// R2-P6-api-0115 (b): a VerifierContext built by hand with a W0 that is
// not a 32-byte scalar, or without L, is refused with an error instead of
// panicking in the scalar encoding.
func TestProcessPake1RefusesAVerifierContextThatDoesNotFit(t *testing.T) {
	t.Parallel()
	salt := testSalt()
	good, err := NewVerifierContext(testPasscode, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	prover, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	wide := new(big.Int).Lsh(big.NewInt(1), 256)
	for name, vc := range map[string]*VerifierContext{
		"nil context":    nil,
		"nil W0":         {W0: nil, L: good.L},
		"W0 of 257 bits": {W0: wide, L: good.L},
		"negative W0":    {W0: big.NewInt(-1), L: good.L},
		"nil L":          {W0: good.W0, L: nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("ProcessPake1 panicked: %v", r)
					}
				}()
				_, err = NewVerifier(vc, nil, nil, nil).ProcessPake1(pA)
			}()
			if !errors.Is(err, ErrInvalidPasscode) {
				t.Errorf("ProcessPake1 = %v, want ErrInvalidPasscode", err)
			}
		})
	}
}
