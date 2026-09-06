// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"errors"
	"testing"
)

// TestCheckValidityRejectsAWrappingNotBefore guards an ACCEPTANCE, not a
// rejection — which is why it matters more than its size suggests.
//
// NotBefore and NotAfter are Matter-epoch seconds (§6.5.1.5) and reaching Unix
// time adds the epoch. A field close to 2^64 wraps that addition to a small
// number, and a small number is one every real clock is already past: the
// window test passes. With NotAfter == 0 nothing else constrains it either,
// because decode.go's ordering check fires only when NotAfter is non-zero.
//
// Measured before the fix: NotBefore = 2^64-1001 became 946683799, and the
// certificate was accepted on an ordinary clock — no exotic device state
// required, unlike the pre-1970 case the doc comment already described.
func TestCheckValidityRejectsAWrappingNotBefore(t *testing.T) {
	t.Parallel()

	v := &Verifier{now: SystemTime{}}
	c := &Certificate{
		NotBefore: ^uint64(0) - 1000, // wraps once the epoch is added
		NotAfter:  0,                 // "never expires", so no ordering check applies
	}

	err := v.checkValidity(c)
	if err == nil {
		t.Fatal("a certificate whose NotBefore wraps the epoch conversion was accepted — the " +
			"wrapped value is smaller than what it stands for, so it passes a window test it " +
			"should fail")
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("err = %v, want ErrMalformed — a field naming no representable time is malformed, "+
			"not expired", err)
	}
}

// TestCheckValidityRejectsAWrappingNotAfter covers the same arithmetic on the
// other bound, where the consequence is milder (an expiry far in the past
// rejects rather than accepts) but the cause is identical.
func TestCheckValidityRejectsAWrappingNotAfter(t *testing.T) {
	t.Parallel()

	v := &Verifier{now: SystemTime{}}
	c := &Certificate{NotBefore: 0, NotAfter: ^uint64(0) - 1000}

	if err := v.checkValidity(c); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed for a NotAfter that wraps the epoch conversion", err)
	}
}

// TestCheckValidityAcceptsOrdinaryBounds is the negative control. Without it
// the two tests above would be satisfied by a checkValidity that rejects
// everything, which would break every real certificate while looking correct
// here.
func TestCheckValidityAcceptsOrdinaryBounds(t *testing.T) {
	t.Parallel()

	v := &Verifier{now: SystemTime{}}
	// Matter-epoch 631152000 is 2020-01-01; NotAfter 0 is the long-lived
	// RCAC convention.
	c := &Certificate{NotBefore: 631152000, NotAfter: 0}

	if err := v.checkValidity(c); err != nil {
		t.Fatalf("an ordinary never-expiring certificate was rejected: %v", err)
	}
}
