// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestOTAUpdateAttemptsDisallowApply holds DisallowApply to its documented
// effect on the apply table (ApplyAllowed): it takes the apply right of
// the offer, so an apply under the offer's token is refused afterwards,
// while an attempt already Applying is still allowed, as the request was
// resent.
func TestOTAUpdateAttemptsDisallowApply(t *testing.T) {
	t.Parallel()
	peer := OTAPeer{FabricIndex: 1, NodeID: 7}
	token := []byte{0x01, 0x02, 0x03, 0x04}
	offered := func(t *testing.T) (*OTAUpdateAttempts, *OTAUpdateAttempt) {
		t.Helper()
		s := NewOTAUpdateAttempts(clocktest.New(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)))
		a := s.Begin(peer)
		if err := s.MakeOffer(a, token, 2); err != nil {
			t.Fatalf("MakeOffer: %v", err)
		}
		if !a.ApplyAllowed() {
			t.Fatal("an offer allows the apply")
		}
		return s, a
	}

	t.Run("refuses the apply of an offer whose apply right was taken", func(t *testing.T) {
		t.Parallel()
		s, a := offered(t)
		s.DisallowApply(a)
		if a.ApplyAllowed() {
			t.Fatal("ApplyAllowed after DisallowApply, want false")
		}
		got, ok := s.ApplyUpdate(peer, token, 2)
		if ok || got != a {
			t.Fatalf("ApplyUpdate = (%v, %v), want (the offer's attempt, refused)", got, ok)
		}
		if a.Ended() {
			t.Fatal("DisallowApply ended the attempt; it only takes the apply right")
		}
	})

	t.Run("still allows the resent apply of an attempt already applying", func(t *testing.T) {
		t.Parallel()
		s, a := offered(t)
		s.SetStatus(a, OTAUpdateApplying, 0)
		s.DisallowApply(a)
		if got, ok := s.ApplyUpdate(peer, token, 2); !ok || got != a {
			t.Fatalf("ApplyUpdate = (%v, %v), want (the attempt, allowed)", got, ok)
		}
	})
}
