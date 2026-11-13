// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"bytes"
	"errors"
	"math/big"
	"testing"
)

// The handshake's secrets are overwritten once they have served or the
// handshake has failed (review II, W1-sec-05). These cases hold the very
// buffers — a struct field's slice, a scalar's words, or what secretHook
// sees just before the overwrite — and read them afterwards. None is
// parallel: they replace secretHook and randomScalarFn.

// held is one buffer the hook saw: the slice itself and what it held.
type held struct {
	name   string
	buf    []byte
	before []byte
}

// holdSecrets records every buffer secretHook sees until the test ends.
func holdSecrets(t *testing.T) *[]held {
	t.Helper()
	var seen []held
	prev := secretHook
	secretHook = func(name string, b []byte) {
		seen = append(seen, held{name: name, buf: b, before: bytes.Clone(b)})
	}
	t.Cleanup(func() { secretHook = prev })
	return &seen
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func wordsZero(w []big.Word) bool {
	for _, v := range w[:cap(w)] {
		if v != 0 {
			return false
		}
	}
	return true
}

// wantOverwritten: each named buffer was seen, held a value, and reads
// zero now.
func wantOverwritten(t *testing.T, seen []held, names ...string) {
	t.Helper()
	for _, name := range names {
		found := false
		for _, h := range seen {
			if h.name != name {
				continue
			}
			found = true
			if len(h.before) == 0 || allZero(h.before) {
				t.Errorf("%s: the hook saw no value (% x)", name, h.before)
			}
			if !allZero(h.buf) {
				t.Errorf("%s: still readable after the handshake step: % x", name, h.buf)
			}
		}
		if !found {
			t.Errorf("%s: never handed to the overwrite", name)
		}
	}
}

// scriptScalars makes randomScalar return the given scalars in order and
// returns their word slices, which share the scalars' storage.
func scriptScalars(t *testing.T, scalars ...*big.Int) [][]big.Word {
	t.Helper()
	words := make([][]big.Word, len(scalars))
	for i, s := range scalars {
		words[i] = s.Bits()
	}
	idx := 0
	prev := randomScalarFn
	randomScalarFn = func() (*big.Int, error) {
		if idx >= len(scalars) {
			return prev()
		}
		idx++
		return scalars[idx-1], nil
	}
	t.Cleanup(func() { randomScalarFn = prev })
	return words
}

func someScalar(fill byte) *big.Int {
	return new(big.Int).SetBytes(bytes.Repeat([]byte{fill}, ScalarSize))
}

// TestHandshakeSecretsOverwrittenAfterSuccess: a completed handshake
// leaves Ke on both sides and nothing else.
func TestHandshakeSecretsOverwrittenAfterSuccess(t *testing.T) {
	seen := holdSecrets(t)
	scalars := scriptScalars(t, someScalar(0x11), someScalar(0x22))
	salt := testSalt()
	vc, err := NewVerifierContext(testPasscode, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	w0Context := new(big.Int).Set(vc.W0)
	prover, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	proverW0, proverW1 := prover.w0.Bits(), prover.w1.Bits()
	if wordsZero(proverW0) || wordsZero(proverW1) {
		t.Fatal("the prover holds no w0 / w1 before the handshake")
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
	// After Pake1: y, Z, V and KcB have served; KcA waits for cA.
	if !wordsZero(scalars[1]) || verifier.y != nil || verifier.zMarshal != nil || verifier.vMarshal != nil {
		t.Error("the verifier still holds y, Z or V after Pake1")
	}
	if !allZero(verifier.kcB) {
		t.Errorf("the verifier still holds KcB after cB was computed: % x", verifier.kcB)
	}
	kcA := verifier.kcA
	if allZero(kcA) {
		t.Fatal("KcA is gone before cA was compared")
	}
	cA, err := prover.ProcessPake2(pake2.Y, pake2.CB)
	if err != nil {
		t.Fatal(err)
	}
	if !wordsZero(scalars[0]) || !wordsZero(proverW0) || !wordsZero(proverW1) {
		t.Error("the prover's x, w0 or w1 is still readable after Pake2")
	}
	if prover.x != nil || prover.w0 != nil || prover.w1 != nil || prover.zMarshal != nil || prover.vMarshal != nil {
		t.Error("the prover still references x, w0, w1, Z or V after Pake2")
	}
	if !allZero(prover.kcA) || !allZero(prover.kcB) {
		t.Error("the prover's confirmation keys are still readable after Pake2")
	}
	if err := verifier.ProcessPake3(cA); err != nil {
		t.Fatal(err)
	}
	if !allZero(kcA) {
		t.Errorf("KcA still readable after cA was compared: % x", kcA)
	}
	wantOverwritten(t, *seen,
		"pbkdf2", "context.w1",
		"prover.x", "prover.w0", "prover.w1", "prover.Z", "prover.V",
		"verifier.y", "verifier.w0", "verifier.Z", "verifier.V",
		"transcript", "kMain", "confirmationKeys")
	// What must live on does: Ke for the session keys, the stored
	// verifier for the next handshake.
	ke := verifier.SharedSecret()
	if len(ke) != SharedSecretSize || allZero(ke) || !bytes.Equal(ke, prover.SharedSecret()) {
		t.Fatalf("Ke after the handshake: verifier % x, prover % x", ke, prover.SharedSecret())
	}
	if vc.W0.Cmp(w0Context) != 0 {
		t.Fatal("the stored verifier's w0 was overwritten; it serves every later handshake")
	}
	again := NewVerifier(vc, nil, nil, nil)
	p2, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pA2, err := p2.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	out2, err := again.ProcessPake1(pA2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.ProcessPake2(out2.Y, out2.CB); err != nil {
		t.Fatalf("a second handshake on the same stored verifier: %v", err)
	}
}

// TestVerifierOverwritesAndEndsAfterFailedConfirmation: a cA that does not
// match takes Ke with it, and the Verifier compares nothing against the
// overwritten KcA — not even the tag an all-zero key would produce.
func TestVerifierOverwritesAndEndsAfterFailedConfirmation(t *testing.T) {
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
	kcA, ke := verifier.kcA, verifier.ke
	if allZero(kcA) || allZero(ke) {
		t.Fatal("no KcA / Ke after Pake1")
	}
	if err := verifier.ProcessPake3(make([]byte, ConfirmTagSize)); !errors.Is(err, ErrConfirmationFailed) {
		t.Fatalf("wrong cA: %v, want ErrConfirmationFailed", err)
	}
	if !allZero(kcA) || !allZero(ke) {
		t.Errorf("after the failed confirmation: KcA % x, Ke % x", kcA, ke)
	}
	if got := verifier.SharedSecret(); got != nil {
		t.Fatalf("SharedSecret after the failed confirmation: % x", got)
	}
	// The tag under the overwritten (all-zero) KcA must not pass.
	forged := hmacSHA256(make([]byte, 16), pake2.Y)[:ConfirmTagSize]
	if err := verifier.ProcessPake3(forged); !errors.Is(err, ErrSessionState) {
		t.Fatalf("a second cA after the failure: %v, want ErrSessionState", err)
	}
	if got := verifier.SharedSecret(); got != nil {
		t.Fatalf("SharedSecret after the forged second cA: % x", got)
	}
}

// TestProverOverwritesAndEndsAfterFailure: a cB that does not match and a
// Y that is no valid point both leave the prover without x, w0, w1 and Ke,
// and it answers no second Pake2.
func TestProverOverwritesAndEndsAfterFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		y    func(good []byte) []byte
		want error
	}{
		{"wrong cB", func(good []byte) []byte { return good }, ErrConfirmationFailed},
		{"invalid Y", func([]byte) []byte { return make([]byte, PointSize) }, ErrInvalidPoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := holdSecrets(t)
			scalars := scriptScalars(t, someScalar(0x33))
			salt := testSalt()
			vc, err := NewVerifierContext(testPasscode, salt, testIterations)
			if err != nil {
				t.Fatal(err)
			}
			prover, err := NewProver(testPasscode, salt, testIterations, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			w0, w1 := prover.w0.Bits(), prover.w1.Bits()
			pA, err := prover.GeneratePake1()
			if err != nil {
				t.Fatal(err)
			}
			pake2, err := NewVerifier(vc, nil, nil, nil).ProcessPake1(pA)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prover.ProcessPake2(tc.y(pake2.Y), make([]byte, ConfirmTagSize)); !errors.Is(err, tc.want) {
				t.Fatalf("ProcessPake2: %v, want %v", err, tc.want)
			}
			if !wordsZero(scalars[0]) || !wordsZero(w0) || !wordsZero(w1) {
				t.Error("x, w0 or w1 still readable after the failed Pake2")
			}
			if !allZero(prover.ke) || !allZero(prover.kcA) || !allZero(prover.kcB) {
				t.Errorf("after the failed Pake2: Ke % x, KcA % x, KcB % x", prover.ke, prover.kcA, prover.kcB)
			}
			wantOverwritten(t, *seen, "prover.x", "prover.w0", "prover.w1")
			if got := prover.SharedSecret(); got != nil {
				t.Fatalf("SharedSecret after the failure: % x", got)
			}
			if _, err := prover.ProcessPake2(pake2.Y, pake2.CB); !errors.Is(err, ErrSessionState) {
				t.Fatalf("a second Pake2 after the failure: %v, want ErrSessionState", err)
			}
		})
	}
}
